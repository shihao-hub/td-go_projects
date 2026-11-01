package service

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"language_projects/go_projects/agyapptitle/internal/config"
	"language_projects/go_projects/agyapptitle/internal/model"
	"language_projects/go_projects/agyapptitle/internal/rpc"
	"language_projects/go_projects/agyapptitle/internal/store"
	"language_projects/go_projects/agyapptitle/internal/titler"
)

type Service struct {
	cfg   *config.Config
	state *store.State
}

func NewService(cfg *config.Config, st *store.State) *Service {
	return &Service{
		cfg:   cfg,
		state: st,
	}
}

// SyncOnce 执行一次全环境扫描与增量汉化。
func (s *Service) SyncOnce(targetEnv string, dryRun bool) ([]model.RenameTask, error) {
	envs := store.GetActiveEnvironments(targetEnv)
	if len(envs) == 0 {
		return nil, fmt.Errorf("no active Antigravity environments found")
	}

	var allTasks []model.RenameTask

	// 探测语言服务器 ConnectRPC 接口
	var lsInfo *rpc.LanguageServerInfo
	for _, env := range envs {
		if env.Name == "app" {
			info, err := rpc.DetectRunningLanguageServer()
			if err == nil {
				lsInfo = info
			}
			break
		}
	}

	for _, env := range envs {
		records, err := store.QuerySessions(env)
		if err != nil {
			log.Printf("[%s] 查询会话数据库异常: %v", env.Name, err)
			continue
		}

		updates := make(map[string]string)
		for _, r := range records {
			// 1. 如果标题是合格中文且不是异常占位语，则无需改写
			if r.IsChinese && !strings.Contains(r.Title, "请提供") {
				continue
			}

			// 2. 检查持久化状态中是否已处理过或已熔断
			if s.state.IsTitled(r.ID) {
				continue
			}
			if !s.state.CanAttempt(r.ID, s.cfg.MaxAttempts) {
				continue
			}

			// 3. 确定提问文本：优先使用 Preview，若为空或为占位语，尝试从本地 transcript.jsonl 中提取真实首问
			prompt := r.Preview
			if strings.TrimSpace(prompt) == "" || strings.Contains(prompt, "请提供") {
				transcriptPath := filepath.Join(env.Path, "brain", r.ID, ".system_generated", "logs", "transcript.jsonl")
				if realPrompt := store.ExtractUserPrompt(transcriptPath); realPrompt != "" {
					prompt = realPrompt
				}
			}

			// 4. 空会话竞争守卫（关键防御）：
			// 当用户刚点击 "+ New Conversation" 时，用户尚未输入任何请求，
			// 此时 preview 为空且无有效旧标题，绝对不能向大模型发送空提示词，直接跳过等待用户首问
			cleanPrompt := strings.TrimSpace(prompt)
			cleanOldTitle := strings.TrimSpace(r.Title)
			if cleanPrompt == "" && (cleanOldTitle == "" || strings.EqualFold(cleanOldTitle, "New Conversation") || strings.EqualFold(cleanOldTitle, "New Chat") || strings.Contains(cleanOldTitle, "请提供")) {
				continue
			}

			// 5. 生成中文标题
			newTitle, err := titler.GenerateTitle(s.cfg, prompt, r.Title)
			if err != nil || newTitle == "" {
				fails := s.state.RecordFailure(r.ID)
				log.Printf("[%s] 会话 %s 第 %d/%d 次生成标题失败: %v", env.Name, r.ID[:8], fails, s.cfg.MaxAttempts, err)
				continue
			}

			task := model.RenameTask{
				SessionID: r.ID,
				OldTitle:  r.Title,
				NewTitle:  newTitle,
				Preview:   prompt,
				Env:       env.Name,
			}
			allTasks = append(allTasks, task)
			updates[r.ID] = newTitle
		}

		if len(updates) > 0 && !dryRun {
			// 1-4. 先执行四重原子更新 (SQLite + raw_summary + .pb + annotations + WAL TRUNCATE)
			count, err := store.ApplyBatchRename(env, updates)
			if err != nil {
				log.Printf("[%s] 写入四重持久化异常: %v", env.Name, err)
			} else {
				log.Printf("[%s] 成功原子写入 %d 条会话中文标题", env.Name, count)
				for cid, t := range updates {
					s.state.RecordSuccess(cid, t)
				}
			}

			// 5. 持久化完成后，再通过 ConnectRPC 实时热推送至 language_server 内存与 UI (仅 app 环境)
			if env.Name == "app" && lsInfo != nil {
				for cid, t := range updates {
					if err := rpc.PushConversationTitle(lsInfo, cid, t); err != nil {
						log.Printf("[%s] ConnectRPC 热推送失败 (%s): %v", env.Name, cid[:8], err)
					} else {
						log.Printf("[%s] ConnectRPC 实时热推送成功: %s -> %s", env.Name, cid[:8], t)
					}
				}
			}
		}
	}

	return allTasks, nil
}

// Watch 启动持续轮询守护。
func (s *Service) Watch(ctx context.Context, interval time.Duration) {
	log.Printf("[agyapptitle] 后台监听已就绪，轮询间隔: %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动立即执行首轮全量扫描
	if tasks, err := s.SyncOnce("all", false); err == nil && len(tasks) > 0 {
		for _, t := range tasks {
			log.Printf("[初始扫描] [%s] %s: %s -> %s", t.Env, t.SessionID[:8], t.OldTitle, t.NewTitle)
		}
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[agyapptitle] 正在优雅关停后台监听...")
			return
		case <-ticker.C:
			tasks, err := s.SyncOnce("all", false)
			if err != nil {
				log.Printf("[轮询异常] %v", err)
				continue
			}
			if len(tasks) > 0 {
				for _, t := range tasks {
					log.Printf("[实时捕获] [%s] %s: %s -> %s", t.Env, t.SessionID[:8], t.OldTitle, t.NewTitle)
				}
			}
		}
	}
}
