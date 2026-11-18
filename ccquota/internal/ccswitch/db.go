// Package ccswitch 只读接入 cc-switch 的 SQLite 库，产出供应商与其用量查询配置。
//
// 硬约束：只读打开（mode=ro），只取需要的列，绝不写入该库或其所在目录。
package ccswitch

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ccquota/internal/appenv"

	_ "modernc.org/sqlite"
)

// DefaultDBPath 返回 cc-switch 默认库路径（~/.cc-switch/cc-switch.db）。
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %w", err)
	}
	return filepath.Join(home, ".cc-switch", "cc-switch.db"), nil
}

// UsageScript 对应 providers.meta.usage_script。
type UsageScript struct {
	Enabled            bool   `json:"enabled"`
	Code               string `json:"code"`
	Timeout            int    `json:"timeout"`
	TemplateType       string `json:"templateType"`
	AutoQueryInterval  int    `json:"autoQueryInterval"`
	CodingPlanProvider string `json:"codingPlanProvider"`
}

// meta 是 providers.meta 中本项目关心的字段。
type meta struct {
	UsageScript *UsageScript `json:"usage_script"`
	AuthBinding *struct {
		Source       string `json:"source"`
		AuthProvider string `json:"authProvider"`
		AccountID    string `json:"accountId"`
	} `json:"authBinding"`
}

// Provider 是只读投影后的供应商信息（不含原始 settings_config，避免密钥扩散）。
type Provider struct {
	ID         string
	AppType    string
	Name       string
	Category   string
	IsCurrent  bool
	APIKey     string
	BaseURL    string
	Usage      *UsageScript
	AccountRef string
	// AuthKind 标识 OAuth 凭据来源（codex_oauth / kimi / agy 等），空表示普通 Key。
	AuthKind string
}

// claudeEnv 是 claude 类供应商 settings_config 里需要的字段。
type claudeEnv struct {
	Env struct {
		AuthToken string `json:"ANTHROPIC_AUTH_TOKEN"`
		BaseURL   string `json:"ANTHROPIC_BASE_URL"`
	} `json:"env"`
}

// Load 读取全部供应商。
//
// 正常情况用只读连接（mode=ro）。若 cc-switch 正在运行且库内含 hot journal，
// 只读连接无权重滚，会返回 SQLITE_READONLY_ROLLBACK；此时复制一份快照到本项目
// 数据目录，在副本上完成恢复与查询，原库一个字节都不碰。
func Load(dbPath string) ([]Provider, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("cc-switch 库不存在: %s", dbPath)
	}
	providers, err := loadFrom(dbPath, true)
	if err == nil {
		return providers, nil
	}
	if !isReadonlyConflict(err) {
		return nil, err
	}
	snap, snapErr := snapshotDB(dbPath)
	if snapErr != nil {
		return nil, fmt.Errorf("%w；复制快照也失败: %v", err, snapErr)
	}
	return loadFrom(snap, false)
}

// isReadonlyConflict 判定是否属于「只读连接无法回滚 hot journal」这类冲突。
func isReadonlyConflict(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "readonly database") ||
		strings.Contains(msg, "readonly rollback") ||
		strings.Contains(msg, "database is locked")
}

// snapshotDB 把库（连同回滚日志）复制到本项目数据目录，返回副本路径。
// 副本用读写连接打开，让 SQLite 自行完成 journal 恢复，不触碰源库。
func snapshotDB(src string) (string, error) {
	dir, err := appenv.EnsureDataDir()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "snapshot.db")
	// 先清掉上一次的副本与日志，避免残留 journal 污染本次读取。
	for _, p := range []string{dst, dst + "-journal", dst + "-wal", dst + "-shm"} {
		_ = os.Remove(p)
	}
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	// delete 模式下未提交事务由 -journal 承载，必须一并复制才能恢复。
	if _, statErr := os.Stat(src + "-journal"); statErr == nil {
		if err := copyFile(src+"-journal", dst+"-journal"); err != nil {
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
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// loadFrom 用指定路径打开并查询；readonly 决定是否加 mode=ro。
func loadFrom(dbPath string, readonly bool) ([]Provider, error) {
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_pragma=busy_timeout(2000)"
	if readonly {
		dsn = "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=busy_timeout(2000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 cc-switch 库失败: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	const query = `SELECT id, app_type, name, settings_config, meta, is_current, category
FROM providers ORDER BY app_type, sort_index`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("读取供应商失败: %w", err)
	}
	defer rows.Close()

	var out []Provider
	for rows.Next() {
		var (
			p              Provider
			settingsConfig string
			metaRaw        string
			current        int
			category       sql.NullString
		)
		if err := rows.Scan(&p.ID, &p.AppType, &p.Name, &settingsConfig, &metaRaw, &current, &category); err != nil {
			return nil, fmt.Errorf("解析供应商行失败: %w", err)
		}
		p.Category = category.String
		p.IsCurrent = current != 0
		fillMeta(&p, metaRaw)
		fillCredentials(&p, settingsConfig)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历供应商失败: %w", err)
	}
	return out, nil
}

func fillMeta(p *Provider, raw string) {
	if raw == "" {
		return
	}
	var m meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	if m.UsageScript != nil && m.UsageScript.Enabled {
		p.Usage = m.UsageScript
	}
	if m.AuthBinding != nil {
		p.AuthKind = m.AuthBinding.AuthProvider
		p.AccountRef = m.AuthBinding.AccountID
	}
}

func fillCredentials(p *Provider, raw string) {
	if raw == "" {
		return
	}
	switch p.AppType {
	case "claude", "claude-desktop":
		var env claudeEnv
		if err := json.Unmarshal([]byte(raw), &env); err == nil {
			p.APIKey = env.Env.AuthToken
			p.BaseURL = env.Env.BaseURL
		}
	case "codex":
		var cfg struct {
			Auth struct {
				OpenAIKey string `json:"OPENAI_API_KEY"`
			} `json:"auth"`
		}
		if err := json.Unmarshal([]byte(raw), &cfg); err == nil {
			p.APIKey = cfg.Auth.OpenAIKey
		}
	}
}
