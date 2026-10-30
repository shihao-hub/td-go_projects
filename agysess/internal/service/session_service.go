package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agysess/internal/model"
	"agysess/internal/store"
	"agysess/internal/terminal"
)

// SessionService 公共业务服务核心，驱动多壳解耦
type SessionService struct {
	agyStore  *store.AgyStore
	metaStore *store.MetaStore
	termMgr   *terminal.TerminalManager
}

// NewSessionService 组装初始化 SessionService
func NewSessionService(customDBPath string, customMetaPath string) (*SessionService, error) {
	agyStore, err := store.NewAgyStore(customDBPath)
	if err != nil {
		return nil, fmt.Errorf("初始化 AgyStore 失败: %w", err)
	}

	metaStore, err := store.NewMetaStore(customMetaPath)
	if err != nil {
		return nil, fmt.Errorf("初始化 MetaStore 失败: %w", err)
	}

	termMgr, err := terminal.NewTerminalManager()
	if err != nil {
		return nil, fmt.Errorf("初始化 TerminalManager 失败: %w", err)
	}

	return &SessionService{
		agyStore:  agyStore,
		metaStore: metaStore,
		termMgr:   termMgr,
	}, nil
}

// DBPath 返回 Agy 数据库路径
func (s *SessionService) DBPath() string {
	return s.agyStore.DBPath()
}

// MetaPath 返回自有元数据路径
func (s *SessionService) MetaPath() string {
	return s.metaStore.FilePath()
}

// ListSessions 获取会话列表并进行过滤、排序与统计计算
func (s *SessionService) ListSessions(ctx context.Context, opts model.FilterOptions) ([]model.Session, model.SessionStats, error) {
	rawSessions, err := s.agyStore.ListAllSessions(ctx)
	if err != nil {
		return nil, model.SessionStats{}, err
	}

	// 探测活跃进程
	runningMap := make(map[string]int)
	if running, err := s.termMgr.DetectRunningSessions(ctx); err == nil {
		for _, r := range running {
			if r.SessionID != "" {
				runningMap[r.SessionID] = r.PID
			}
		}
	}

	var allSessions []model.Session
	var stats model.SessionStats
	stats.Total = len(rawSessions)

	// 聚合元数据与运行状态
	for _, sess := range rawSessions {
		meta, hasMeta := s.metaStore.GetMeta(sess.ID)
		if hasMeta {
			sess.IsArchived = meta.IsArchived
			sess.ArchivedAt = meta.ArchivedAt
			sess.CustomNotes = meta.CustomNotes
			sess.CustomTitle = meta.CustomTitle
		}

		if pid, isRun := runningMap[sess.ID]; isRun {
			sess.IsRunning = true
			sess.PID = pid
			stats.Running++
		}

		if sess.IsArchived {
			stats.Archived++
		} else {
			stats.Active++
		}

		allSessions = append(allSessions, sess)
	}

	// 过滤流程
	var filtered []model.Session
	queryLower := strings.ToLower(strings.TrimSpace(opts.Query))
	for _, sess := range allSessions {
		// 归档过滤
		if opts.ArchivedOnly && !sess.IsArchived {
			continue
		}
		if !opts.IncludeArchived && !opts.ArchivedOnly && sess.IsArchived {
			continue
		}

		// 项目过滤
		if opts.ProjectID != "" && !strings.EqualFold(sess.ProjectID, opts.ProjectID) {
			continue
		}

		// 关键词搜索（支持标题、自定义标题、预览内容、ID）
		if queryLower != "" {
			matched := strings.Contains(strings.ToLower(sess.Title), queryLower) ||
				strings.Contains(strings.ToLower(sess.CustomTitle), queryLower) ||
				strings.Contains(strings.ToLower(sess.Preview), queryLower) ||
				strings.Contains(strings.ToLower(sess.ID), queryLower) ||
				strings.Contains(strings.ToLower(sess.ProjectID), queryLower)
			if !matched {
				continue
			}
		}

		filtered = append(filtered, sess)
	}

	// 排序流程
	sortBy := opts.SortBy
	if sortBy == "" {
		sortBy = "time_desc"
	}

	sort.Slice(filtered, func(i, j int) bool {
		switch sortBy {
		case "time_asc":
			return filtered[i].LastModifiedTime.Before(filtered[j].LastModifiedTime)
		case "steps_desc":
			return filtered[i].StepCount > filtered[j].StepCount
		case "steps_asc":
			return filtered[i].StepCount < filtered[j].StepCount
		case "title_asc":
			return filtered[i].DisplayTitle() < filtered[j].DisplayTitle()
		case "time_desc":
			fallthrough
		default:
			return filtered[i].LastModifiedTime.After(filtered[j].LastModifiedTime)
		}
	})

	// 分页截断
	totalFiltered := len(filtered)
	start := opts.Offset
	if start < 0 {
		start = 0
	}
	if start > totalFiltered {
		return []model.Session{}, stats, nil
	}

	end := totalFiltered
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}

	return filtered[start:end], stats, nil
}

// GetSession 获取单条会话详情
func (s *SessionService) GetSession(ctx context.Context, sessionID string) (*model.Session, error) {
	sess, err := s.agyStore.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("未找到 ID 为 [%s] 的会话", sessionID)
	}

	meta, hasMeta := s.metaStore.GetMeta(sess.ID)
	if hasMeta {
		sess.IsArchived = meta.IsArchived
		sess.ArchivedAt = meta.ArchivedAt
		sess.CustomNotes = meta.CustomNotes
		sess.CustomTitle = meta.CustomTitle
	}

	if pid, ok := s.termMgr.GetPIDForSession(sess.ID); ok {
		sess.IsRunning = true
		sess.PID = pid
	} else if running, err := s.termMgr.DetectRunningSessions(ctx); err == nil {
		for _, r := range running {
			if r.SessionID == sess.ID {
				sess.IsRunning = true
				sess.PID = r.PID
				break
			}
		}
	}

	return sess, nil
}

// ArchiveSession 归档指定会话
func (s *SessionService) ArchiveSession(ctx context.Context, sessionID string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.metaStore.SetArchived(sess.ID, true)
}

// UnarchiveSession 取消归档指定会话
func (s *SessionService) UnarchiveSession(ctx context.Context, sessionID string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.metaStore.SetArchived(sess.ID, false)
}

// UpdateNotes 更新会话备注
func (s *SessionService) UpdateNotes(ctx context.Context, sessionID string, notes string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.metaStore.SetNotes(sess.ID, notes)
}

// UpdateCustomTitle 更新会话自定义标题
func (s *SessionService) UpdateCustomTitle(ctx context.Context, sessionID string, title string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.metaStore.SetCustomTitle(sess.ID, title)
}

// OpenSession 在 Windows Terminal 中打开/恢复会话
func (s *SessionService) OpenSession(ctx context.Context, sessionID string, newWindow bool) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.termMgr.OpenConversation(ctx, sess, newWindow)
}

// CreateSession 在 Windows Terminal 中创建新会话
func (s *SessionService) CreateSession(ctx context.Context, opts model.CreateSessionOptions) error {
	return s.termMgr.CreateNewConversation(ctx, opts)
}

// GetRunningSessions 探测所有正在运行的 agy 会话
func (s *SessionService) GetRunningSessions(ctx context.Context) ([]terminal.RunningSessionInfo, error) {
	return s.termMgr.DetectRunningSessions(ctx)
}

// KillSession 关闭指定运行中会话的终端窗口与进程
func (s *SessionService) KillSession(ctx context.Context, sessionID string) (int, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	return s.termMgr.KillSessionProcess(ctx, sess.ID)
}

// ListProjects 提取系统中存在的所有项目 ID 列表
func (s *SessionService) ListProjects(ctx context.Context) ([]string, error) {
	sessions, err := s.agyStore.ListAllSessions(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var projects []string
	for _, sess := range sessions {
		p := strings.TrimSpace(sess.ProjectID)
		if p != "" && !seen[p] {
			seen[p] = true
			projects = append(projects, p)
		}
	}
	sort.Strings(projects)
	return projects, nil
}
