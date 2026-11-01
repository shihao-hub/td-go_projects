package service

import (
	"context"
	"fmt"
	"log"
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
			if r.IsChinese {
				continue
			}

			// 检查持久化状态中是否已处理过或已熔断
			if s.state.IsTitled(r.ID) {
				continue
			}
			if !s.state.CanAttempt(r.ID, s.cfg.MaxAttempts) {
				continue
			}

			// 生成中文标题
			newTitle, err := titler.GenerateTitle(s.cfg, r.Preview, r.Title)
			if err != nil || newTitle == "" {
				fails := s.state.RecordFailure(r.ID)
				log.Printf("[%s] 会话 %s 第 %d/%d 次生成标题失败: %v", env.Name, r.ID[:8], fails, s.cfg.MaxAttempts, err)
				continue
			}

			task := model.RenameTask{
				SessionID: r.ID,
				OldTitle:  r.Title,
				NewTitle:  newTitle,
				Preview:   r.Preview,
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

// Watch 启动前台持续监听，并在捕获到新会话时自动执行汉化与推送。
func (s *Service) Watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[agyapptitle] 后台监听已就绪，轮询间隔: %v", interval)

	// 启动立即执行一次初始化同步
	_, _ = s.SyncOnce("all", false)

	for {
		select {
		case <-ctx.Done():
			log.Println("[agyapptitle] 监听器优雅停止")
			return
		case <-ticker.C:
			tasks, err := s.SyncOnce("all", false)
			if err != nil {
				continue
			}
			for _, t := range tasks {
				log.Printf("[实时捕获] [%s] %s: %s -> %s", t.Env, t.SessionID[:8], t.OldTitle, t.NewTitle)
			}
		}
	}
}
