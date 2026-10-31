// Package zedthreads 管理 Zed sidebar_threads 的会话归属（agent_id）与归档状态。
//
// 背景：Zed 的 Agent 面板把「会话」绑定在 sidebar_threads.agent_id 上，该字段
// 取值就是 settings.json 里 agent_servers 的键名。把历史会话的 agent_id 改写成
// 代理条目（如 "Google Antigravity (auto-title)"），即可让旧会话改由共享 daemon
// 承载；Zed 本地不保存对话消息，重开时一律走 ACP session/load 由 agent 侧回放，
// 因此只要 session_id 不变、后端能读到同一份会话库，历史就是完整的。
//
// 写库遵循 zedhub 的三条铁律：Zed 未退出不写、写前做一致性备份、默认 dry-run。
package zedthreads

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Thread 是 sidebar_threads 中供迁移使用的一行（只取必要列，禁止 SELECT *）。
type Thread struct {
	ThreadID    []byte
	SessionID   string
	AgentID     string
	Title       string
	Archived    bool
	FolderPaths string
	UpdatedAt   string
}

// AgentStat 是单个 agent_id 的会话统计。
type AgentStat struct {
	AgentID     string
	Total       int
	Active      int
	Archived    int
	NoSessionID int
	LastUpdated string
}

// ListOptions 控制 List 的过滤条件。
type ListOptions struct {
	Agent    string // 空表示不过滤
	Archived string // all | active | archived
	Limit    int    // <=0 表示不限
}

// Plan 是一次写操作的完整计划（dry-run 与 apply 共用同一份数据）。
type Plan struct {
	Kind     string // adopt | archive
	From     string
	To       string
	Rows     []Thread
	EmptySID int // 因无 session_id 被跳过的行数
	Skipped  int // 因已归档/无变化被跳过的行数
}

// Result 是写操作的结果。
type Result struct {
	Updated int
	Backup  string
}

// DiscoverDBPath 定位 Zed 的 SQLite 库：优先 0-stable，否则取最近修改的 channel。
func DiscoverDBPath() (string, error) {
	base := filepath.Join(os.Getenv("LOCALAPPDATA"), "Zed", "db")
	stable := filepath.Join(base, "0-stable", "db.sqlite")
	if _, err := os.Stat(stable); err == nil {
		return stable, nil
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return "", fmt.Errorf("未找到 Zed 数据库目录 %s: %w", base, err)
	}

	type candidate struct {
		path string
		mod  time.Time
	}
	var found []candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name(), "db.sqlite")
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		found = append(found, candidate{path: p, mod: st.ModTime()})
	}
	if len(found) == 0 {
		return "", fmt.Errorf("Zed 数据库不存在：%s", stable)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	return found[0].path, nil
}

// OpenRO 以只读方式打开 Zed 库（Zed 运行中也能安全读取）。
func OpenRO(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return db, nil
}

// Agents 返回按会话数降序的 agent 分布。
func Agents(db *sql.DB) ([]AgentStat, error) {
	const q = `SELECT agent_id,
       COUNT(*),
       SUM(CASE WHEN archived = 0 THEN 1 ELSE 0 END),
       SUM(CASE WHEN archived = 1 THEN 1 ELSE 0 END),
       SUM(CASE WHEN session_id IS NULL OR session_id = '' THEN 1 ELSE 0 END),
       MAX(updated_at)
  FROM sidebar_threads
 GROUP BY agent_id
 ORDER BY COUNT(*) DESC`

	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("查询 agent 分布失败: %w", err)
	}
	defer rows.Close()

	var out []AgentStat
	for rows.Next() {
		var (
			agent                  sql.NullString
			total, active, arch, n int
			last                   sql.NullString
		)
		if err := rows.Scan(&agent, &total, &active, &arch, &n, &last); err != nil {
			return nil, fmt.Errorf("读取 agent 分布失败: %w", err)
		}
		out = append(out, AgentStat{
			AgentID:     agent.String,
			Total:       total,
			Active:      active,
			Archived:    arch,
			NoSessionID: n,
			LastUpdated: last.String,
		})
	}
	return out, rows.Err()
}

// List 按过滤条件列出会话（按更新时间降序）。
func List(db *sql.DB, opts ListOptions) ([]Thread, error) {
	var (
		where []string
		args  []any
	)
	if opts.Agent != "" {
		where = append(where, "agent_id = ?")
		args = append(args, opts.Agent)
	}
	switch strings.ToLower(opts.Archived) {
	case "", "all":
	case "active":
		where = append(where, "archived = 0")
	case "archived":
		where = append(where, "archived = 1")
	default:
		return nil, fmt.Errorf("无效的 --archived 取值 %q（all|active|archived）", opts.Archived)
	}

	q := `SELECT thread_id, COALESCE(session_id, ''), COALESCE(agent_id, ''), title,
                 archived, COALESCE(folder_paths, ''), COALESCE(updated_at, '')
            FROM sidebar_threads`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY updated_at DESC"
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}
	defer rows.Close()

	var out []Thread
	for rows.Next() {
		var (
			t   Thread
			arc int
		)
		if err := rows.Scan(&t.ThreadID, &t.SessionID, &t.AgentID, &t.Title, &arc, &t.FolderPaths, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取会话失败: %w", err)
		}
		t.Archived = arc != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// PlanAdopt 生成「把 from 名下会话过户到 to」的计划。
//
// 只处理有 session_id 的行：没有 session_id 的空 thread 无会话可载，单独计数后跳过，
// 由调用方决定是否额外归档（archiveEmpty）。
func PlanAdopt(db *sql.DB, from, to string, includeArchived bool) (*Plan, error) {
	if from == "" || to == "" {
		return nil, errors.New("--from 与 --to 不能为空")
	}
	if from == to {
		return nil, fmt.Errorf("--from 与 --to 相同（%s），无需迁移", from)
	}

	rows, err := List(db, ListOptions{Agent: from, Archived: "all"})
	if err != nil {
		return nil, err
	}

	plan := &Plan{Kind: "adopt", From: from, To: to}
	for _, t := range rows {
		if t.Archived && !includeArchived {
			plan.Skipped++
			continue
		}
		if t.SessionID == "" {
			plan.EmptySID++
			continue
		}
		plan.Rows = append(plan.Rows, t)
	}
	return plan, nil
}

// PlanArchive 生成「归档 agent 名下全部未归档会话」的计划。
func PlanArchive(db *sql.DB, agent string) (*Plan, error) {
	if agent == "" {
		return nil, errors.New("--agent 不能为空")
	}
	rows, err := List(db, ListOptions{Agent: agent, Archived: "active"})
	if err != nil {
		return nil, err
	}
	return &Plan{Kind: "archive", From: agent, Rows: rows}, nil
}

// ZedRunning 判断 Zed 是否在运行。写库前必须为 false。
func ZedRunning() (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq Zed.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		// tasklist 缺失或权限受限时不能断定 Zed 未运行，交给调用方决定。
		return false, fmt.Errorf("无法检查 Zed 进程: %w", err)
	}
	return strings.Contains(strings.ToLower(string(out)), "zed.exe"), nil
}

// Apply 执行计划：进程检查 → 一致性备份 → 单事务写入 → checkpoint → 复查。
func Apply(dbPath string, plan *Plan) (*Result, error) {
	if plan == nil || len(plan.Rows) == 0 {
		return &Result{}, nil
	}

	if running, err := ZedRunning(); err != nil {
		return nil, err
	} else if running {
		return nil, errors.New("Zed 正在运行：请完全退出 Zed（含托盘与所有窗口）后重试；运行中写库会被内存缓存覆盖")
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", filepath.ToSlash(dbPath)))
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	backup, err := Backup(db, dbPath)
	if err != nil {
		return nil, err
	}

	updated, err := applyTx(db, plan)
	if err != nil {
		return nil, err
	}

	// checkpoint 需要独占锁，必须在事务提交之后做；失败不影响已提交的数据。
	_, _ = db.Exec("PRAGMA wal_checkpoint(PASSIVE)")

	return &Result{Updated: updated, Backup: backup}, nil
}

// Backup 用 VACUUM INTO 生成一致性快照（自动合并 WAL），失败时回退到整文件复制。
func Backup(db *sql.DB, dbPath string) (string, error) {
	dir := filepath.Join(os.Getenv("APPDATA"), "language_projects", "acp-title-proxy", "backups",
		"threads-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	dest := filepath.Join(dir, "db.sqlite")

	stmt := "VACUUM INTO '" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := db.Exec(stmt); err == nil {
		return dest, nil
	}

	// 回退：复制主库与 WAL/SHM（仅在 SQLite 不支持 VACUUM INTO 时走到）。
	if err := copyFile(dbPath, dest); err != nil {
		return "", fmt.Errorf("备份失败: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyFile(dbPath+suffix, dest+suffix); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("备份 %s 失败: %w", suffix, err)
			}
		}
	}
	return dest, nil
}

// applyTx 在单个事务内执行计划；抽出以便测试（不做进程检查与备份）。
func applyTx(db *sql.DB, plan *Plan) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var updated int
	for _, t := range plan.Rows {
		var res sql.Result
		var err error
		switch plan.Kind {
		case "adopt":
			res, err = tx.Exec("UPDATE sidebar_threads SET agent_id = ? WHERE thread_id = ?", plan.To, t.ThreadID)
		case "archive":
			res, err = tx.Exec("UPDATE sidebar_threads SET archived = 1 WHERE thread_id = ?", t.ThreadID)
		default:
			return 0, fmt.Errorf("未知的计划类型: %s", plan.Kind)
		}
		if err != nil {
			return 0, fmt.Errorf("更新会话 %s 失败: %w", t.SessionID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return updated, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
