package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agysess/internal/model"

	_ "modernc.org/sqlite"
)

// AgyStore 只读读取 Agy 官方 conversation_summaries.db
type AgyStore struct {
	dbPath string
}

// NewAgyStore 实例化 AgyStore，自动定位默认数据库路径或使用自定义路径
func NewAgyStore(customPath string) (*AgyStore, error) {
	path := customPath
	if path == "" {
		path = os.Getenv("AGYSESS_DB_PATH")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("无法获取用户家目录: %w", err)
		}
		// 优先定位 ~/.gemini/antigravity-cli/conversation_summaries.db
		path = filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// 回退检查 ~/.gemini/antigravity/conversation_summaries.db
			desktopPath := filepath.Join(home, ".gemini", "antigravity", "conversation_summaries.db")
			if _, err2 := os.Stat(desktopPath); err2 == nil {
				path = desktopPath
			}
		}
	}

	return &AgyStore{dbPath: path}, nil
}

// DBPath 获取当前连接的数据库路径
func (s *AgyStore) DBPath() string {
	return s.dbPath
}

// OpenRO 打开只读数据库连接，使用后必须关闭
func (s *AgyStore) OpenRO() (*sql.DB, error) {
	if _, err := os.Stat(s.dbPath); err != nil {
		return nil, fmt.Errorf("Agy 会话数据库文件不存在 [%s]: %w", s.dbPath, err)
	}

	// modernc.org/sqlite 只读 URI 格式
	// 避免对正在写库的 agy CLI 造成文件锁争用
	dsn := fmt.Sprintf("file:%s?mode=ro", filepath.ToSlash(s.dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 Agy 只读数据库失败: %w", err)
	}

	return db, nil
}

// ListAllSessions 查询所有原生会话（严格禁止 SELECT *）
func (s *AgyStore) ListAllSessions(ctx context.Context) ([]model.Session, error) {
	db, err := s.OpenRO()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// 显式声明查询字段，对齐仓库强约束
	query := `
		SELECT
			conversation_id,
			title,
			preview,
			step_count,
			last_modified_time,
			workspace_uris,
			status,
			project_id,
			agent_name
		FROM conversation_summaries
		ORDER BY last_modified_time DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("执行会话列表查询失败: %w", err)
	}
	defer rows.Close()

	var sessions []model.Session
	for rows.Next() {
		var (
			id, title, preview string
			stepCount          int
			timeStr            string
			workspaceURIs      string
			status, projectID  string
			agentName          string
		)

		if err := rows.Scan(
			&id,
			&title,
			&preview,
			&stepCount,
			&timeStr,
			&workspaceURIs,
			&status,
			&projectID,
			&agentName,
		); err != nil {
			return nil, fmt.Errorf("读取会话数据行失败: %w", err)
		}

		parsedTime := parseFlexibleTime(timeStr)

		sessions = append(sessions, model.Session{
			ID:               id,
			Title:            title,
			Preview:          preview,
			StepCount:        stepCount,
			LastModifiedTime: parsedTime,
			WorkspaceURIs:    cleanWorkspaceURIs(workspaceURIs),
			Status:           status,
			ProjectID:        projectID,
			AgentName:        agentName,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历会话数据发生错误: %w", err)
	}

	return sessions, nil
}

// GetSessionByID 按 ID 查询单条原生会话（严格禁止 SELECT *）
func (s *AgyStore) GetSessionByID(ctx context.Context, sessionID string) (*model.Session, error) {
	db, err := s.OpenRO()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT
			conversation_id,
			title,
			preview,
			step_count,
			last_modified_time,
			workspace_uris,
			status,
			project_id,
			agent_name
		FROM conversation_summaries
		WHERE conversation_id = ? OR conversation_id LIKE ?
		LIMIT 1
	`

	var (
		id, title, preview string
		stepCount          int
		timeStr            string
		workspaceURIs      string
		status, projectID  string
		agentName          string
	)

	prefixPattern := sessionID + "%"
	err = db.QueryRowContext(ctx, query, sessionID, prefixPattern).Scan(
		&id,
		&title,
		&preview,
		&stepCount,
		&timeStr,
		&workspaceURIs,
		&status,
		&projectID,
		&agentName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询会话详情失败 [%s]: %w", sessionID, err)
	}

	parsedTime := parseFlexibleTime(timeStr)

	return &model.Session{
		ID:               id,
		Title:            title,
		Preview:          preview,
		StepCount:        stepCount,
		LastModifiedTime: parsedTime,
		WorkspaceURIs:    cleanWorkspaceURIs(workspaceURIs),
		Status:           status,
		ProjectID:        projectID,
		AgentName:        agentName,
	}, nil
}

// cleanSingleURI 清洗单个 URI 字符串
func cleanSingleURI(raw string) string {
	res := strings.TrimSpace(raw)
	res = strings.Trim(res, "\"")
	res = strings.TrimPrefix(res, "file:///")
	res = strings.TrimPrefix(res, "file://")
	res = strings.TrimPrefix(res, "file:\\\\\\")
	res = strings.TrimPrefix(res, "file:\\\\")
	res = strings.TrimPrefix(res, "file:\\")
	if decoded, err := url.PathUnescape(res); err == nil {
		res = decoded
	}
	return filepath.FromSlash(res)
}

// cleanWorkspaceURIs 清洗去除 URI 前缀如 file:/// 或 JSON 数组字符串
func cleanWorkspaceURIs(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	// 尝试作为 JSON 数组解析
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var list []string
		if err := json.Unmarshal([]byte(trimmed), &list); err == nil && len(list) > 0 {
			var cleaned []string
			for _, item := range list {
				if c := cleanSingleURI(item); c != "" {
					cleaned = append(cleaned, c)
				}
			}
			return strings.Join(cleaned, "; ")
		}
	}

	return cleanSingleURI(trimmed)
}

// parseFlexibleTime 兼容 SQLite 中各种时间格式
func parseFlexibleTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.9999999-07:00",
		"2006-01-02 15:04:05.9999999+00:00",
		"2006-01-02 15:04:05.9999999",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
