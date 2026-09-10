package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，免 CGO
)

// timeFmt 时间存储格式：UTC + 固定 9 位纳秒，保证字符串排序即时间排序（RFC3339 兼容）
const timeFmt = "2006-01-02T15:04:05.000000000Z"

// 工具状态
const (
	StatusActive  = "active"
	StatusInvalid = "invalid"
)

// Tool 注册的工具（list/info 的 JSON 数据形状）
type Tool struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Path        string          `json:"path"`
	Description string          `json:"description"`
	Status      string          `json:"status"`                // active / invalid，输出前现场 stat 刷新
	Meta        json.RawMessage `json:"meta,omitempty"`        // 白名单约束的扩展字段，空则省略
	AddedAt     string          `json:"added_at"`
	SizeBytes   int64           `json:"size_bytes"`            // 运行时 stat
	LaunchCount int64           `json:"launch_count"`          // JOIN launches 统计
	LastLaunch  string          `json:"last_launch,omitempty"` // 从未启动则省略
}

// Launch 一次启动记录；DurationMs/ExitCode 为 NULL（未正常结束）时输出 null
type Launch struct {
	ID         int64  `json:"id"`
	ToolID     int64  `json:"tool_id"`
	StartedAt  string `json:"started_at"`
	DurationMs *int64 `json:"duration_ms"`
	ExitCode   *int   `json:"exit_code"`
}

// 领域错误
var ErrNotFound = errors.New("tool not found")

// ConflictError 唯一性冲突（Field 为 "name" / "path"）
type ConflictError struct{ Field string }

func (e *ConflictError) Error() string { return "conflict: " + e.Field }

const schema = `
CREATE TABLE IF NOT EXISTS tools (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  path        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'active'
              CHECK (status IN ('active','invalid')),
  meta        TEXT,
  added_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS launches (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tool_id     INTEGER NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  started_at  TEXT NOT NULL,
  duration_ms INTEGER,
  exit_code   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_launches_tool
  ON launches(tool_id, started_at DESC);
`

const toolSelect = `SELECT t.id, t.name, t.path, t.description, t.status, t.meta, t.added_at,
       (SELECT COUNT(*) FROM launches l WHERE l.tool_id = t.id)          AS launch_count,
       (SELECT MAX(l.started_at) FROM launches l WHERE l.tool_id = t.id) AS last_launch
FROM tools t `

// Store SQLite 存储
type Store struct{ db *sql.DB }

type scanner interface{ Scan(dest ...any) error }

// dbPath 数据库路径：%AppData%\clictl\clictl.db，取不到回退 ~/.clictl/
func dbPath() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "clictl", "clictl.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位数据目录: %w", err)
	}
	return filepath.Join(home, ".clictl", "clictl.db"), nil
}

// Open 打开数据库（WAL + busy_timeout=2s + 外键约束）并完成建表迁移
func Open() (*Store, error) {
	path, err := dbPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库
func (s *Store) Close() error { return s.db.Close() }

// NormalizeName 调用名统一小写存储/比较
func NormalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// AddTool 注册新工具。name 统一小写、path 转 Clean 后的绝对路径；
// meta 写入前必须过 ValidateMeta 白名单校验。
func (s *Store) AddTool(name, path, desc string, meta json.RawMessage) (Tool, error) {
	norm, err := ValidateMeta(meta)
	if err != nil {
		return Tool{}, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return Tool{}, fmt.Errorf("解析路径失败: %w", err)
	}
	abs = filepath.Clean(abs)

	var metaVal any // nil 或规范化后的 JSON 字符串
	if len(norm) > 0 {
		metaVal = string(norm)
	}

	_, err = s.db.Exec(
		`INSERT INTO tools (name, path, description, status, meta, added_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		NormalizeName(name), abs, desc, StatusActive, metaVal, time.Now().UTC().Format(timeFmt),
	)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "tools.name"):
			return Tool{}, &ConflictError{Field: "name"}
		case strings.Contains(msg, "tools.path"):
			return Tool{}, &ConflictError{Field: "path"}
		}
		return Tool{}, fmt.Errorf("写入失败: %w", err)
	}
	return s.GetTool(name)
}

// RemoveTool 删除注册（外键级联删除其 launches），返回删除条数
func (s *Store) RemoveTool(name string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM tools WHERE name = ?`, NormalizeName(name))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	return n, nil
}

// SetMeta 整体替换 meta（内部先过白名单校验），返回更新后的工具
func (s *Store) SetMeta(name string, meta json.RawMessage) (Tool, error) {
	norm, err := ValidateMeta(meta)
	if err != nil {
		return Tool{}, err
	}
	var metaVal any
	if len(norm) > 0 {
		metaVal = string(norm)
	}
	res, err := s.db.Exec(`UPDATE tools SET meta = ? WHERE name = ?`, metaVal, NormalizeName(name))
	if err != nil {
		return Tool{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Tool{}, ErrNotFound
	}
	return s.GetTool(name)
}

// RefreshStatus 回写工具的上次校验状态
func (s *Store) RefreshStatus(id int64, valid bool) error {
	st := StatusInvalid
	if valid {
		st = StatusActive
	}
	_, err := s.db.Exec(`UPDATE tools SET status = ? WHERE id = ?`, st, id)
	return err
}

// GetTool 按名查询；输出前现场校验文件状态并回写
func (s *Store) GetTool(name string) (Tool, error) {
	t, err := scanTool(s.db.QueryRow(toolSelect + ` WHERE t.name = ?`, NormalizeName(name)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tool{}, ErrNotFound
		}
		return Tool{}, err
	}
	s.refreshTool(&t)
	return t, nil
}

// ListTools 列出工具，按 launch_count 降序、name 升序；status 空串 = 不过滤。
// 每次调用先现场校验所有工具文件状态并回写，保证 --status 过滤基于实时结论。
func (s *Store) ListTools(status string) ([]Tool, error) {
	if err := s.syncAllStatuses(); err != nil {
		return nil, err
	}

	q := toolSelect
	var args []any
	if status != "" {
		q += ` WHERE t.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY launch_count DESC, t.name ASC`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tools := []Tool{}
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// syncAllStatuses 已保证落库 status 最新，这里只需补 SizeBytes
	for i := range tools {
		if fi, err := os.Stat(tools[i].Path); err == nil && !fi.IsDir() {
			tools[i].SizeBytes = fi.Size()
		}
	}
	return tools, nil
}

// InsertLaunch 记录一次启动的开始
func (s *Store) InsertLaunch(toolID int64, startedAt time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO launches (tool_id, started_at) VALUES (?, ?)`,
		toolID, startedAt.UTC().Format(timeFmt),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishLaunch 回写启动记录的耗时与退出码
func (s *Store) FinishLaunch(id int64, durMs int64, exitCode int) error {
	_, err := s.db.Exec(`UPDATE launches SET duration_ms = ?, exit_code = ? WHERE id = ?`, durMs, exitCode, id)
	return err
}

// RecentLaunches 最近 n 条启动记录（新→旧）
func (s *Store) RecentLaunches(toolID int64, n int) ([]Launch, error) {
	rows, err := s.db.Query(
		`SELECT id, tool_id, started_at, duration_ms, exit_code
		 FROM launches WHERE tool_id = ?
		 ORDER BY started_at DESC, id DESC LIMIT ?`, toolID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	launches := []Launch{}
	for rows.Next() {
		var l Launch
		if err := rows.Scan(&l.ID, &l.ToolID, &l.StartedAt, &l.DurationMs, &l.ExitCode); err != nil {
			return nil, err
		}
		launches = append(launches, l)
	}
	return launches, rows.Err()
}

// TotalDuration 正常结束的启动次数与累计耗时（毫秒）
func (s *Store) TotalDuration(toolID int64) (count int64, totalMs int64, err error) {
	err = s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(duration_ms), 0)
		 FROM launches WHERE tool_id = ? AND duration_ms IS NOT NULL`, toolID,
	).Scan(&count, &totalMs)
	return
}

// refreshTool 现场校验单个工具：刷新 Status/SizeBytes，与落库值不一致时回写
func (s *Store) refreshTool(t *Tool) {
	live := StatusInvalid
	var size int64
	if fi, err := os.Stat(t.Path); err == nil && !fi.IsDir() {
		live = StatusActive
		size = fi.Size()
	}
	if live != t.Status {
		_ = s.RefreshStatus(t.ID, live == StatusActive)
	}
	t.Status = live
	t.SizeBytes = size
}

// syncAllStatuses 现场校验全部工具的文件状态，与落库值不一致时回写
func (s *Store) syncAllStatuses() error {
	rows, err := s.db.Query(`SELECT id, path, status FROM tools`)
	if err != nil {
		return err
	}
	type rec struct {
		id     int64
		path   string
		status string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.path, &r.status); err != nil {
			rows.Close()
			return err
		}
		recs = append(recs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, r := range recs {
		live := StatusInvalid
		if fi, err := os.Stat(r.path); err == nil && !fi.IsDir() {
			live = StatusActive
		}
		if live != r.status {
			if _, err := s.db.Exec(`UPDATE tools SET status = ? WHERE id = ?`, live, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanTool(sc scanner) (Tool, error) {
	var (
		t         Tool
		meta      sql.NullString
		lastLaunch sql.NullString
	)
	err := sc.Scan(&t.ID, &t.Name, &t.Path, &t.Description, &t.Status, &meta, &t.AddedAt, &t.LaunchCount, &lastLaunch)
	if err != nil {
		return Tool{}, err
	}
	if meta.Valid && meta.String != "" {
		t.Meta = json.RawMessage(meta.String)
	}
	if lastLaunch.Valid {
		t.LastLaunch = lastLaunch.String
	}
	return t, nil
}
