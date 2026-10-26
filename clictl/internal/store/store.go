package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"clictl/internal/appinfo"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，免 CGO
)

// TimeFmt 时间存储格式：UTC + 固定 9 位纳秒，保证字符串排序即时间排序（RFC3339 兼容）。
// 导出供 runner 复用（start 输出 started_at 需与库内格式一致）
const TimeFmt = "2006-01-02T15:04:05.000000000Z"

// 工具状态
const (
	StatusActive  = "active"
	StatusInvalid = "invalid"
)

// 卡片形态
const (
	KindExe    = "exe"
	KindSource = "source"
	KindBoth   = "both"
)

// 启动目标
const (
	TargetExe    = "exe"
	TargetSource = "source"
)

// 源码启动类型（主模式提示）
const (
	SourceTypeGUI     = "gui"
	SourceTypeConsole = "console"
)

// 启动模式（launches.mode）
const (
	ModeRun      = "run"
	ModeConsole  = "console"
	ModeHidden   = "hidden"
	ModeDetached = "detached"
)

// ExeTarget 卡片的可执行文件目标
type ExeTarget struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // active / invalid
	SizeBytes int64  `json:"size_bytes"`
	Console   bool   `json:"console"` // 由 service 按 PE Subsystem 填充
}

// SourceTarget 卡片的源码命令目标
type SourceTarget struct {
	Program string   `json:"program"`
	Workdir string   `json:"workdir"`
	Args    []string `json:"args"`
	Type    string   `json:"type"` // gui / console
	Status  string   `json:"status"`
	Command string   `json:"command"` // 展示用：uv run zedhub-gui
}

// Tool 注册的工具卡片（list/info 的 JSON 数据形状）
type Tool struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Status        string          `json:"status"` // active / invalid，输出前现场校验
	Meta          json.RawMessage `json:"meta,omitempty"`
	AddedAt       string          `json:"added_at"`
	DefaultTarget string          `json:"default_target"`
	Exe           *ExeTarget      `json:"exe,omitempty"`
	Source        *SourceTarget   `json:"source,omitempty"`

	// 兼容字段：path = exe 路径，无 exe 时取源码程序
	Path        string `json:"path"`
	Kind        string `json:"kind"` // exe / source / both
	SizeBytes   int64  `json:"size_bytes"`
	LaunchCount int64  `json:"launch_count"`
	LastLaunch  string `json:"last_launch,omitempty"`

	dbStatus string // 扫描时的落库状态，用于判断是否需要回写
}

// Launch 一次启动记录；DurationMs/ExitCode/PID 为 NULL 时输出 null。
type Launch struct {
	ID         int64  `json:"id"`
	ToolID     int64  `json:"tool_id"`
	StartedAt  string `json:"started_at"`
	DurationMs *int64 `json:"duration_ms"`
	ExitCode   *int   `json:"exit_code"`
	PID        *int   `json:"pid"`
	Target     string `json:"target,omitempty"` // exe / source
	Mode       string `json:"mode,omitempty"`   // run / console / hidden / detached
}

// 领域错误
var ErrNotFound = errors.New("tool not found")

// ConflictError 唯一性冲突（Field 为 name / exe_path / source）
type ConflictError struct{ Field string }

func (e *ConflictError) Error() string { return "conflict: " + e.Field }

// ValidationError 输入不合法，Code 为 JSON 错误码
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// ExeSpec 注册/编辑时的 exe 目标输入
type ExeSpec struct {
	Path string
}

// SourceSpec 注册/编辑时的源码目标输入
type SourceSpec struct {
	Program string
	Workdir string
	Args    []string
	Type    string
}

// AddSpec 新建卡片（exe 与 source 至少一个）
type AddSpec struct {
	Name          string
	Description   string
	Meta          json.RawMessage
	DefaultTarget string
	Exe           *ExeSpec
	Source        *SourceSpec
}

// UpdateSpec 全量编辑卡片；Exe/Source 为 nil 表示删除该目标
type UpdateSpec struct {
	Description   string
	Meta          json.RawMessage
	DefaultTarget string
	Exe           *ExeSpec
	Source        *SourceSpec
}

const toolsV3Columns = `
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  name            TEXT NOT NULL UNIQUE COLLATE NOCASE,
  description     TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','invalid')),
  meta            TEXT,
  added_at        TEXT NOT NULL,
  default_target  TEXT NOT NULL DEFAULT 'exe'
                  CHECK (default_target IN ('exe','source')),
  exe_path        TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
  source_program  TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
  source_workdir  TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
  source_args     TEXT NOT NULL DEFAULT '[]',
  source_type     TEXT NOT NULL DEFAULT 'gui'
                  CHECK (source_type IN ('gui','console'))
`

// schemaTables 只建表，不建依赖新列的索引：旧库先迁移，再执行 schemaIndexes。
const schemaTables = `
CREATE TABLE IF NOT EXISTS tools (` + toolsV3Columns + `);
CREATE TABLE IF NOT EXISTS launches (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tool_id     INTEGER NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
  started_at  TEXT NOT NULL,
  duration_ms INTEGER,
  exit_code   INTEGER,
  pid         INTEGER,
  target      TEXT,
  mode        TEXT
);
`

const schemaIndexes = `
CREATE INDEX IF NOT EXISTS idx_launches_tool
  ON launches(tool_id, started_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tools_exe_path
  ON tools(exe_path) WHERE exe_path <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_tools_source
  ON tools(source_program, source_workdir, source_args)
  WHERE source_program <> '';
`

const toolSelect = `SELECT t.id, t.name, t.description, t.status, t.meta, t.added_at,
       t.default_target, t.exe_path, t.source_program, t.source_workdir,
       t.source_args, t.source_type,
       (SELECT COUNT(*) FROM launches l WHERE l.tool_id = t.id)          AS launch_count,
       (SELECT MAX(l.started_at) FROM launches l WHERE l.tool_id = t.id) AS last_launch
FROM tools t `

// Store SQLite 存储
type Store struct{ db *sql.DB }

type scanner interface{ Scan(dest ...any) error }

// Open 打开当前构建通道的数据库（dev / prod 目录隔离）
func Open() (*Store, error) {
	return OpenAt(appinfo.DefaultDBPath())
}

// OpenAt 打开指定路径的数据库（WAL + busy_timeout=2s + 外键约束）并完成建表迁移
func OpenAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if _, err := db.Exec(schemaTables); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移表结构失败: %w", err)
	}
	if _, err := db.Exec(schemaIndexes); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建索引失败: %w", err)
	}
	return &Store{db: db}, nil
}

// migrate 存量库迁移：
//  1. v2.x → v3：tools 增加 default_target/exe_path/source_*，path 搬进 exe_path；
//  2. launches 补 pid（v1.2.0）、target/mode（v2.2.0）。
func migrate(db *sql.DB) error {
	if err := migrateToolsV3(db); err != nil {
		return err
	}
	return migrateLaunches(db)
}

func migrateToolsV3(db *sql.DB) error {
	cols, err := tableColumns(db, "tools")
	if err != nil {
		return err
	}
	if cols["default_target"] && cols["exe_path"] && cols["source_program"] {
		return nil
	}
	if !cols["path"] {
		return fmt.Errorf("tools 表缺少 path 列，无法迁移到 v3")
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// PRAGMA foreign_keys 只能在事务外切换；DROP tools 时不能级联删 launches。
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE tools_new (`+toolsV3Columns+`)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tools_new
		  (id, name, description, status, meta, added_at, default_target,
		   exe_path, source_program, source_workdir, source_args, source_type)
		SELECT id, name, description, status, meta, added_at, 'exe',
		       path, '', '', '[]', 'gui'
		FROM tools`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE tools`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tools_new RENAME TO tools`); err != nil {
		return err
	}
	// AUTOINCREMENT 序列随表名迁移（旧表序列随 DROP 删除）
	if _, err := tx.ExecContext(ctx,
		`UPDATE sqlite_sequence SET name='tools' WHERE name='tools_new'`); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateLaunches(db *sql.DB) error {
	cols, err := tableColumns(db, "launches")
	if err != nil {
		return err
	}
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"pid", `ALTER TABLE launches ADD COLUMN pid INTEGER`},
		{"target", `ALTER TABLE launches ADD COLUMN target TEXT`},
		{"mode", `ALTER TABLE launches ADD COLUMN mode TEXT`},
	} {
		if cols[col.name] {
			continue
		}
		if _, err := db.Exec(col.ddl); err != nil {
			return err
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// Close 关闭数据库
func (s *Store) Close() error { return s.db.Close() }

// NormalizeName 调用名统一小写存储/比较
func NormalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// AddTool 新建卡片（exe / 源码 / 双目标）
func (s *Store) AddTool(spec AddSpec) (Tool, error) {
	name := NormalizeName(spec.Name)
	if name == "" {
		return Tool{}, &ValidationError{Code: "bad_args", Message: "name 不能为空"}
	}
	meta, exe, source, def, err := normalizeSpec(spec.Name, spec.Meta, spec.DefaultTarget, spec.Exe, spec.Source)
	if err != nil {
		return Tool{}, err
	}
	if err := s.checkName(name); err != nil {
		return Tool{}, err
	}
	if exe != nil {
		if err := s.checkExePath(exe.Path, 0); err != nil {
			return Tool{}, err
		}
	}
	srcArgs := "[]"
	if source != nil {
		srcArgs = marshalArgs(source.Args)
		if err := s.checkSource(source.Program, source.Workdir, srcArgs, 0); err != nil {
			return Tool{}, err
		}
	}

	_, err = s.db.Exec(
		`INSERT INTO tools
		   (name, description, status, meta, added_at, default_target,
		    exe_path, source_program, source_workdir, source_args, source_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, spec.Description, StatusActive, nullMeta(meta), time.Now().UTC().Format(TimeFmt),
		def, exePath(exe), sourceProgram(source), sourceWorkdir(source), srcArgs, sourceType(source),
	)
	if err != nil {
		return Tool{}, mapConflict(err)
	}
	return s.GetTool(name)
}

// AttachExe 给已有卡片附加 exe 目标；已有 exe 时报冲突。附加后默认目标切到 exe。
func (s *Store) AttachExe(name string, spec ExeSpec) (Tool, error) {
	tool, err := s.GetTool(name)
	if err != nil {
		return Tool{}, err
	}
	if tool.Exe != nil {
		return Tool{}, &ConflictError{Field: "exe_path"}
	}
	exe, err := normalizeExeSpec(&spec)
	if err != nil {
		return Tool{}, err
	}
	if exe == nil {
		return Tool{}, &ValidationError{Code: "bad_args", Message: "exe 路径不能为空"}
	}
	if err := s.checkExePath(exe.Path, tool.ID); err != nil {
		return Tool{}, err
	}
	if _, err := s.db.Exec(
		`UPDATE tools SET exe_path = ?, default_target = ? WHERE id = ?`,
		exe.Path, TargetExe, tool.ID,
	); err != nil {
		return Tool{}, mapConflict(err)
	}
	return s.GetTool(name)
}

// AttachSource 给已有卡片附加源码目标；已有源码目标时报冲突。默认目标保持不变。
func (s *Store) AttachSource(name string, spec SourceSpec) (Tool, error) {
	tool, err := s.GetTool(name)
	if err != nil {
		return Tool{}, err
	}
	if tool.Source != nil {
		return Tool{}, &ConflictError{Field: "source"}
	}
	source, err := normalizeSourceSpec(&spec)
	if err != nil {
		return Tool{}, err
	}
	if source == nil {
		return Tool{}, &ValidationError{Code: "bad_args", Message: "源码程序不能为空"}
	}
	srcArgs := marshalArgs(source.Args)
	if err := s.checkSource(source.Program, source.Workdir, srcArgs, tool.ID); err != nil {
		return Tool{}, err
	}
	if _, err := s.db.Exec(
		`UPDATE tools
		 SET source_program = ?, source_workdir = ?, source_args = ?, source_type = ?
		 WHERE id = ?`,
		source.Program, source.Workdir, srcArgs, source.Type, tool.ID,
	); err != nil {
		return Tool{}, mapConflict(err)
	}
	return s.GetTool(name)
}

// UpdateTool 全量编辑卡片
func (s *Store) UpdateTool(name string, spec UpdateSpec) (Tool, error) {
	tool, err := s.GetTool(name)
	if err != nil {
		return Tool{}, err
	}
	meta, exe, source, def, err := normalizeSpec(name, spec.Meta, spec.DefaultTarget, spec.Exe, spec.Source)
	if err != nil {
		return Tool{}, err
	}
	if exe != nil {
		if err := s.checkExePath(exe.Path, tool.ID); err != nil {
			return Tool{}, err
		}
	}
	srcArgs := "[]"
	if source != nil {
		srcArgs = marshalArgs(source.Args)
		if err := s.checkSource(source.Program, source.Workdir, srcArgs, tool.ID); err != nil {
			return Tool{}, err
		}
	}
	if _, err := s.db.Exec(
		`UPDATE tools
		 SET description = ?, meta = ?, default_target = ?,
		     exe_path = ?, source_program = ?, source_workdir = ?,
		     source_args = ?, source_type = ?
		 WHERE id = ?`,
		spec.Description, nullMeta(meta), def,
		exePath(exe), sourceProgram(source), sourceWorkdir(source),
		srcArgs, sourceType(source), tool.ID,
	); err != nil {
		return Tool{}, mapConflict(err)
	}
	return s.GetTool(name)
}

// SetMeta 整体替换 meta（内部先过白名单校验），返回更新后的工具
func (s *Store) SetMeta(name string, meta json.RawMessage) (Tool, error) {
	norm, err := ValidateMeta(meta)
	if err != nil {
		return Tool{}, err
	}
	res, err := s.db.Exec(`UPDATE tools SET meta = ? WHERE name = ?`, nullMeta(norm), NormalizeName(name))
	if err != nil {
		return Tool{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Tool{}, ErrNotFound
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

// RefreshStatus 回写工具的上次校验状态（总体：任一目标有效即 active）
func (s *Store) RefreshStatus(id int64, valid bool) error {
	st := StatusInvalid
	if valid {
		st = StatusActive
	}
	_, err := s.db.Exec(`UPDATE tools SET status = ? WHERE id = ?`, st, id)
	return err
}

// GetTool 按名查询；输出前现场校验目标状态并回写
func (s *Store) GetTool(name string) (Tool, error) {
	t, err := scanTool(s.db.QueryRow(toolSelect+` WHERE t.name = ?`, NormalizeName(name)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Tool{}, ErrNotFound
		}
		return Tool{}, err
	}
	s.refreshTool(&t)
	return t, nil
}

// ListTools 列出工具，按 name 升序；status 空串 = 不过滤。
// 每次调用先现场校验所有工具目标状态并回写，保证 --status 过滤基于实时结论。
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
	q += ` ORDER BY t.name ASC`

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
	return tools, nil
}

// InsertLaunch 记录一次启动的开始
func (s *Store) InsertLaunch(toolID int64, startedAt time.Time, target, mode string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO launches (tool_id, started_at, target, mode) VALUES (?, ?, ?, ?)`,
		toolID, startedAt.UTC().Format(TimeFmt), nullString(target), nullString(mode),
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

// SetLaunchPID 回写启动记录的子进程 PID（start 后台启动专用）
func (s *Store) SetLaunchPID(id int64, pid int) error {
	_, err := s.db.Exec(`UPDATE launches SET pid = ? WHERE id = ?`, pid, id)
	return err
}

const launchCols = `id, tool_id, started_at, duration_ms, exit_code, pid, target, mode`

func scanLaunch(sc scanner) (Launch, error) {
	var (
		l      Launch
		target sql.NullString
		mode   sql.NullString
	)
	if err := sc.Scan(&l.ID, &l.ToolID, &l.StartedAt, &l.DurationMs, &l.ExitCode, &l.PID, &target, &mode); err != nil {
		return Launch{}, err
	}
	if target.Valid {
		l.Target = target.String
	}
	if mode.Valid {
		l.Mode = mode.String
	}
	return l, nil
}

// RecentLaunches 最近 n 条启动记录（新→旧）
func (s *Store) RecentLaunches(toolID int64, n int) ([]Launch, error) {
	rows, err := s.db.Query(
		`SELECT `+launchCols+`
		 FROM launches WHERE tool_id = ?
		 ORDER BY started_at DESC, id DESC LIMIT ?`, toolID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	launches := []Launch{}
	for rows.Next() {
		l, err := scanLaunch(rows)
		if err != nil {
			return nil, err
		}
		launches = append(launches, l)
	}
	return launches, rows.Err()
}

// UnfinishedStarts 该工具所有未闭环的后台启动记录
// （pid 非空且 duration_ms 为空，即 start 拉起后从未回写结束），旧→新排序。
// 是否仍在运行由调用方现场探活判定（pid 可能已被系统回收复用）
func (s *Store) UnfinishedStarts(toolID int64) ([]Launch, error) {
	rows, err := s.db.Query(
		`SELECT `+launchCols+`
		 FROM launches
		 WHERE tool_id = ? AND pid IS NOT NULL AND duration_ms IS NULL
		 ORDER BY started_at ASC, id ASC`, toolID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	launches := []Launch{}
	for rows.Next() {
		l, err := scanLaunch(rows)
		if err != nil {
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

// refreshTool 现场校验单个工具；与落库状态不一致时回写
func (s *Store) refreshTool(t *Tool) {
	evaluateTargets(t)
	if t.Status != t.dbStatus {
		_ = s.RefreshStatus(t.ID, t.Status == StatusActive)
		t.dbStatus = t.Status
	}
}

// syncAllStatuses 现场校验全部工具的总体状态，与落库值不一致时回写
func (s *Store) syncAllStatuses() error {
	rows, err := s.db.Query(`SELECT id, status, exe_path, source_program, source_workdir FROM tools`)
	if err != nil {
		return err
	}
	type rec struct {
		id            int64
		status        string
		exePath       string
		sourceProgram string
		sourceWorkdir string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.status, &r.exePath, &r.sourceProgram, &r.sourceWorkdir); err != nil {
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
		if fileExists(r.exePath) || (r.sourceProgram != "" && fileExists(r.sourceProgram) && dirExists(r.sourceWorkdir)) {
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
		t          Tool
		meta       sql.NullString
		lastLaunch sql.NullString
		defTarget  string
		exePath    string
		srcProgram string
		srcWorkdir string
		srcArgs    string
		srcType    string
	)
	err := sc.Scan(&t.ID, &t.Name, &t.Description, &t.Status, &meta, &t.AddedAt,
		&defTarget, &exePath, &srcProgram, &srcWorkdir, &srcArgs, &srcType,
		&t.LaunchCount, &lastLaunch)
	if err != nil {
		return Tool{}, err
	}
	if meta.Valid && meta.String != "" {
		t.Meta = json.RawMessage(meta.String)
	}
	if lastLaunch.Valid {
		t.LastLaunch = lastLaunch.String
	}
	t.DefaultTarget = defTarget
	if exePath != "" {
		t.Exe = &ExeTarget{Path: exePath}
	}
	if srcProgram != "" {
		var args []string
		if srcArgs != "" {
			_ = json.Unmarshal([]byte(srcArgs), &args)
		}
		if args == nil {
			args = []string{}
		}
		t.Source = &SourceTarget{
			Program: srcProgram,
			Workdir: srcWorkdir,
			Args:    args,
			Type:    srcType,
			Command: sourceCommand(srcProgram, args),
		}
	}
	switch {
	case t.Exe != nil && t.Source != nil:
		t.Kind = KindBoth
	case t.Exe != nil:
		t.Kind = KindExe
	case t.Source != nil:
		t.Kind = KindSource
	}
	t.Path = exePath
	if t.Path == "" {
		t.Path = srcProgram
	}
	t.dbStatus = t.Status
	evaluateTargets(&t)
	return t, nil
}

// evaluateTargets 现场计算两侧目标状态与 exe 大小，并汇总总体 status
func evaluateTargets(t *Tool) {
	t.SizeBytes = 0
	exeValid, srcValid := false, false
	if t.Exe != nil {
		if fi, err := os.Stat(t.Exe.Path); err == nil && !fi.IsDir() {
			exeValid = true
			t.Exe.Status = StatusActive
			t.Exe.SizeBytes = fi.Size()
			t.SizeBytes = fi.Size()
		} else {
			t.Exe.Status = StatusInvalid
			t.Exe.SizeBytes = 0
		}
	}
	if t.Source != nil {
		if fileExists(t.Source.Program) && dirExists(t.Source.Workdir) {
			srcValid = true
			t.Source.Status = StatusActive
		} else {
			t.Source.Status = StatusInvalid
		}
	}
	if exeValid || srcValid {
		t.Status = StatusActive
	} else {
		t.Status = StatusInvalid
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// normalizeSpec 统一校验/规范化 Add/Update 输入
func normalizeSpec(_ string, rawMeta json.RawMessage, defTarget string, exeSpec *ExeSpec, srcSpec *SourceSpec) (json.RawMessage, *ExeSpec, *SourceSpec, string, error) {
	meta, err := ValidateMeta(rawMeta)
	if err != nil {
		return nil, nil, nil, "", err
	}
	exe, err := normalizeExeSpec(exeSpec)
	if err != nil {
		return nil, nil, nil, "", err
	}
	source, err := normalizeSourceSpec(srcSpec)
	if err != nil {
		return nil, nil, nil, "", err
	}
	def, err := resolveDefaultTarget(defTarget, exe != nil, source != nil)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return meta, exe, source, def, nil
}

func normalizeExeSpec(spec *ExeSpec) (*ExeSpec, error) {
	if spec == nil {
		return nil, nil
	}
	p := strings.TrimSpace(spec.Path)
	if p == "" {
		return nil, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, &ValidationError{Code: "bad_args", Message: "解析 exe 路径失败: " + err.Error()}
	}
	abs = filepath.Clean(abs)
	if strings.ToLower(filepath.Ext(abs)) != ".exe" {
		return nil, &ValidationError{Code: "not_exe", Message: "仅支持 .exe 文件: " + abs}
	}
	return &ExeSpec{Path: abs}, nil
}

func normalizeSourceSpec(spec *SourceSpec) (*SourceSpec, error) {
	if spec == nil {
		return nil, nil
	}
	program := strings.TrimSpace(spec.Program)
	if program == "" {
		return nil, nil
	}
	if !filepath.IsAbs(program) {
		resolved, err := exec.LookPath(program)
		if err != nil {
			return nil, &ValidationError{Code: "program_not_found", Message: "找不到程序: " + program}
		}
		program = resolved
	}
	abs, err := filepath.Abs(program)
	if err != nil {
		return nil, &ValidationError{Code: "bad_args", Message: "解析程序路径失败: " + err.Error()}
	}
	abs = filepath.Clean(abs)
	if strings.ToLower(filepath.Ext(abs)) != ".exe" {
		return nil, &ValidationError{Code: "not_exe", Message: "源码启动程序必须是 .exe: " + abs}
	}

	workdir := strings.TrimSpace(spec.Workdir)
	if workdir == "" {
		return nil, &ValidationError{Code: "bad_args", Message: "源码启动必须指定工作目录"}
	}
	workAbs, err := filepath.Abs(workdir)
	if err != nil {
		return nil, &ValidationError{Code: "bad_args", Message: "解析工作目录失败: " + err.Error()}
	}
	workAbs = filepath.Clean(workAbs)

	typ := strings.ToLower(strings.TrimSpace(spec.Type))
	if typ == "" {
		typ = SourceTypeGUI
	}
	if typ != SourceTypeGUI && typ != SourceTypeConsole {
		return nil, &ValidationError{Code: "bad_args", Message: "源码类型仅支持 gui | console"}
	}

	args := spec.Args
	if args == nil {
		args = []string{}
	}
	if len(args) > 64 {
		return nil, &ValidationError{Code: "bad_args", Message: "固定参数最多 64 项"}
	}
	for _, a := range args {
		if strings.ContainsRune(a, 0) {
			return nil, &ValidationError{Code: "bad_args", Message: "固定参数不能包含 NUL 字符"}
		}
		if len(a) > 1024 {
			return nil, &ValidationError{Code: "bad_args", Message: "单个固定参数不能超过 1024 字节"}
		}
	}

	return &SourceSpec{Program: abs, Workdir: workAbs, Args: args, Type: typ}, nil
}

func resolveDefaultTarget(requested string, hasExe, hasSource bool) (string, error) {
	if !hasExe && !hasSource {
		return "", &ValidationError{Code: "bad_args", Message: "exe 与源码命令至少配置一个"}
	}
	r := strings.ToLower(strings.TrimSpace(requested))
	switch r {
	case "":
		if hasExe {
			return TargetExe, nil
		}
		return TargetSource, nil
	case TargetExe:
		if hasExe {
			return TargetExe, nil
		}
		if hasSource {
			return TargetSource, nil
		}
	case TargetSource:
		if hasSource {
			return TargetSource, nil
		}
		if hasExe {
			return TargetExe, nil
		}
	default:
		return "", &ValidationError{Code: "bad_args", Message: "default_target 仅支持 exe | source"}
	}
	return "", &ValidationError{Code: "bad_args", Message: "default_target 指向不存在的目标"}
}

func (s *Store) checkName(name string) error {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM tools WHERE name = ?`, name).Scan(&one)
	if err == nil {
		return &ConflictError{Field: "name"}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) checkExePath(path string, excludeID int64) error {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM tools WHERE exe_path = ? AND exe_path <> '' AND id <> ?`,
		path, excludeID,
	).Scan(&one)
	if err == nil {
		return &ConflictError{Field: "exe_path"}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) checkSource(program, workdir, argsJSON string, excludeID int64) error {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM tools
		 WHERE source_program = ? AND source_workdir = ? AND source_args = ? AND id <> ?`,
		program, workdir, argsJSON, excludeID,
	).Scan(&one)
	if err == nil {
		return &ConflictError{Field: "source"}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "tools.name"):
		return &ConflictError{Field: "name"}
	case strings.Contains(msg, "idx_tools_exe_path"), strings.Contains(msg, "tools.exe_path"):
		return &ConflictError{Field: "exe_path"}
	case strings.Contains(msg, "idx_tools_source"):
		return &ConflictError{Field: "source"}
	}
	return fmt.Errorf("写入失败: %w", err)
}

func marshalArgs(args []string) string {
	if args == nil {
		args = []string{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func sourceCommand(program string, args []string) string {
	parts := append([]string{filepath.Base(program)}, args...)
	return strings.Join(parts, " ")
}

func exePath(spec *ExeSpec) string {
	if spec == nil {
		return ""
	}
	return spec.Path
}

func sourceProgram(spec *SourceSpec) string {
	if spec == nil {
		return ""
	}
	return spec.Program
}

func sourceWorkdir(spec *SourceSpec) string {
	if spec == nil {
		return ""
	}
	return spec.Workdir
}

func sourceType(spec *SourceSpec) string {
	if spec == nil {
		return SourceTypeGUI
	}
	return spec.Type
}

func nullMeta(meta json.RawMessage) any {
	if len(meta) == 0 {
		return nil
	}
	return string(meta)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
