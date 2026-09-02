package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// modelSel 对应 opencode 中 session.model 的 JSON:
// {"id":"glm-5.3","providerID":"zai-coding-plan","variant":"max"}
// variant 即思考档位（effort），由 cc-switch 等供应商 variant 方案定义。
type modelSel struct {
	Provider string `json:"providerID"`
	ID       string `json:"id"`
	Variant  string `json:"variant"`
}

// 数据完整性分级
const (
	modeBroken = 0 // session 表/关键列缺失，无法统计
	modeBasic  = 1 // 仅 session 表可用，只能展示当前模型
	modeFull   = 2 // session + message + event 齐全，可还原启动模型
)

type schemaInfo struct {
	mode int
	note string
}

type store struct {
	db            *sql.DB
	sc            schemaInfo
	path          string
	usingSnapshot bool
}

func (s *store) Close() error { return s.db.Close() }

// openStore 只读打开 opencode.db；当 WAL 无 -shm 的窗口期导致只读打开失败时，
// 退化为临时目录快照副本（best-effort），不打扰正在运行的 opencode。
func openStore(ctx context.Context, path string) (*store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("数据库不存在: %s（用 -db 指定路径）", abs)
	}
	db, sc, err := openRO(ctx, abs)
	if err == nil {
		return &store{db: db, sc: sc, path: abs}, nil
	}
	snapPath, serr := snapshotDB(abs)
	if serr != nil {
		return nil, fmt.Errorf("打开失败: %v（快照兜底也失败: %v）", err, serr)
	}
	db2, sc2, err2 := openRO(ctx, snapPath)
	if err2 != nil {
		return nil, fmt.Errorf("打开失败: %v", err)
	}
	return &store{db: db2, sc: sc2, path: abs, usingSnapshot: true}, nil
}

func openRO(ctx context.Context, path string) (*sql.DB, schemaInfo, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, schemaInfo{}, err
	}
	sc, err := detectSchema(ctx, db)
	if err != nil {
		db.Close()
		return nil, schemaInfo{}, fmt.Errorf("detectSchema: %w", err)
	}
	if sc.mode >= modeBasic {
		var n int
		if err := db.QueryRowContext(ctx, "select count(*) from session").Scan(&n); err != nil {
			db.Close()
			return nil, sc, fmt.Errorf("probe session: %w", err)
		}
	}
	return db, sc, nil
}

// dsn 生成只读 URI。注意必须是 file:///C:/... 三斜杠形式：
// 两斜杠会被 SQLite 当作 authority（主机名）解析导致打开失败，
// 无斜杠的 file:C:/ 在部分版本不受理。
func dsn(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := &url.URL{Scheme: "file", Path: p}
	return u.String() + "?mode=ro&_pragma=busy_timeout(5000)"
}

// detectSchema 探测必需表/列，opencode 升级改表结构时分级降级而非 panic。
func detectSchema(ctx context.Context, db *sql.DB) (schemaInfo, error) {
	tables := map[string]bool{}
	rows, err := db.QueryContext(ctx, "select name from sqlite_master where type='table'")
	if err != nil {
		return schemaInfo{}, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return schemaInfo{}, err
		}
		tables[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return schemaInfo{}, err
	}

	var missing []string
	colsOf := func(table string) map[string]bool {
		cols := map[string]bool{}
		cr, err := db.QueryContext(ctx, "select name from pragma_table_info(?)", table)
		if err != nil {
			return cols
		}
		defer cr.Close()
		for cr.Next() {
			var name string
			if err := cr.Scan(&name); err != nil {
				return cols
			}
			cols[name] = true
		}
		return cols
	}
	check := func(table string, need ...string) bool {
		if !tables[table] {
			missing = append(missing, "缺少表 "+table)
			return false
		}
		cols := colsOf(table)
		for _, c := range need {
			if !cols[c] {
				missing = append(missing, fmt.Sprintf("表 %s 缺少列 %s", table, c))
				return false
			}
		}
		return true
	}

	if !check("session", "id", "model", "time_created") {
		return schemaInfo{mode: modeBroken, note: fmt.Sprintf(
			"opencode 库结构无法识别（%s），可能是 opencode 更新改了表结构，请升级 ocstat。检测到的表: %s",
			strings.Join(missing, "; "), strings.Join(sortedKeys(tables), ", "))}, nil
	}
	if check("message", "session_id", "data", "time_created") && check("event", "type", "data") {
		return schemaInfo{mode: modeFull}, nil
	}
	return schemaInfo{mode: modeBasic, note: "事件/消息表不可用，仅展示会话当前模型（无法还原启动档位）: " + strings.Join(missing, "; ")}, nil
}

func snapshotDB(src string) (string, error) {
	dir := filepath.Join(os.TempDir(), "ocstat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, fmt.Sprintf("snap-%d.db", os.Getpid()))
	for _, suffix := range []string{"", "-wal", "-shm"} {
		s := src + suffix
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if err := copyFile(s, dst+suffix); err != nil {
			return "", err
		}
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type sessionStat struct {
	Version  string
	Created  int64
	MsgCount int
	Startup  *modelSel
	Current  *modelSel
}

type dataSnapshot struct {
	Generated     time.Time
	DBPath        string
	DBSize        int64
	DBMod         time.Time
	UsingSnapshot bool
	Mode          int
	Note          string
	Versions      []string
	Sessions      []*sessionStat
	NoMsgCount    int
	StartupCount  int
	EffortCfg     effortConfig
	CfgMerged     bool
}

type eventEntry struct {
	t int64
	m modelSel
}

func (s *store) snapshot(ctx context.Context) (*dataSnapshot, error) {
	if s.sc.mode == modeBroken {
		return nil, errors.New(s.sc.note)
	}
	snap := &dataSnapshot{
		Generated:     time.Now(),
		DBPath:        s.path,
		UsingSnapshot: s.usingSnapshot,
		Mode:          s.sc.mode,
		Note:          s.sc.note,
	}
	if fi, err := os.Stat(s.path); err == nil {
		snap.DBSize, snap.DBMod = fi.Size(), fi.ModTime()
	}
	snap.EffortCfg, snap.CfgMerged = loadEffortConfig()

	stats := map[string]*sessionStat{}
	verSet := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, "select id, model, version, time_created from session")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var ver, model sql.NullString
		var created int64
		if err := rows.Scan(&id, &model, &ver, &created); err != nil {
			rows.Close()
			return nil, err
		}
		st := &sessionStat{Created: created, Current: parseModelJSON(model)}
		if ver.Valid {
			st.Version = ver.String
			verSet[ver.String] = true
		}
		stats[id] = st
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.sc.mode == modeFull {
		if err := enrichFull(ctx, s.db, stats); err != nil {
			return nil, err
		}
	}

	list := make([]*sessionStat, 0, len(stats))
	for _, st := range stats {
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created > list[j].Created })
	snap.Sessions = list
	for _, st := range list {
		if s.sc.mode == modeFull {
			if st.MsgCount == 0 {
				snap.NoMsgCount++
			}
			if st.Startup != nil {
				snap.StartupCount++
			}
		}
	}
	snap.Versions = sortedKeys(verSet)
	return snap, nil
}

// enrichFull 用 message 表找到每个会话首条非 title 的 assistant 消息，
// 再用 session.created/updated 事件时间线对齐出该时刻生效的模型+档位（已验证 235/235 吻合）。
func enrichFull(ctx context.Context, db *sql.DB, stats map[string]*sessionStat) error {
	type firstMsg struct {
		t int64
	}
	first := map[string]*firstMsg{}
	counts := map[string]int{}
	rows, err := db.QueryContext(ctx, "select session_id, data, time_created from message order by time_created")
	if err != nil {
		return err
	}
	for rows.Next() {
		var sid, data string
		var t int64
		if err := rows.Scan(&sid, &data, &t); err != nil {
			rows.Close()
			return err
		}
		counts[sid]++
		var d struct {
			Role    string `json:"role"`
			Agent   string `json:"agent"`
			ModelID string `json:"modelID"`
		}
		if json.Unmarshal([]byte(data), &d) == nil && d.Role == "assistant" && d.Agent != "title" && d.ModelID != "" && first[sid] == nil {
			first[sid] = &firstMsg{t: t}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	timeline := map[string][]eventEntry{}
	erows, err := db.QueryContext(ctx, "select data from event where type in ('session.created.1','session.updated.1') order by rowid")
	if err != nil {
		return err
	}
	for erows.Next() {
		var data string
		if err := erows.Scan(&data); err != nil {
			erows.Close()
			return err
		}
		var e struct {
			SessionID string `json:"sessionID"`
			Info      *struct {
				Model *modelSel `json:"model"`
				Time  *struct {
					Updated int64 `json:"updated"`
				} `json:"time"`
			} `json:"info"`
		}
		if json.Unmarshal([]byte(data), &e) != nil || e.Info == nil || e.Info.Model == nil || e.Info.Time == nil || e.Info.Time.Updated == 0 {
			continue
		}
		timeline[e.SessionID] = append(timeline[e.SessionID], eventEntry{t: e.Info.Time.Updated, m: *e.Info.Model})
	}
	erows.Close()
	if err := erows.Err(); err != nil {
		return err
	}

	for id, st := range stats {
		st.MsgCount = counts[id]
		tl := timeline[id]
		sort.SliceStable(tl, func(i, j int) bool { return tl[i].t < tl[j].t })
		if f := first[id]; f != nil {
			var hit *modelSel
			for i := range tl {
				if tl[i].t <= f.t {
					m := tl[i].m
					hit = &m
				} else {
					break
				}
			}
			if hit == nil && len(tl) > 0 {
				m := tl[0].m
				hit = &m
			}
			st.Startup = hit
		}
	}
	return nil
}

// parseModelJSON 容错解析 model 列：JSON 对象 / 纯字符串 / null 均兼容。
func parseModelJSON(v sql.NullString) *modelSel {
	if !v.Valid {
		return nil
	}
	s := strings.TrimSpace(v.String)
	if s == "" || s == "null" {
		return nil
	}
	var m modelSel
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return &modelSel{ID: s}
	}
	if m.ID == "" {
		return nil
	}
	return &m
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
