// Package meta 实现 PROJECT.toml 元数据的定义、校验、加载与固定顺序落盘。
// 解析用 BurntSushi/toml（解析难），落盘用手写发射器（发射易，且可输出组头注释）。
package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// FileName 是每个项目根目录下元数据文件的固定名称。
const FileName = "PROJECT.toml"

// Stage 表示项目所处阶段；空串表示未标注。
type Stage string

// ValidStages 是 stage 字段的全部合法取值。
var ValidStages = []Stage{"idea", "learning", "wip", "mvp", "usable", "paused", "dropped"}

// Meta 是 PROJECT.toml 的完整数据模型。
// 三个布尔字段用 *bool 表达三态：nil = 未标注，区别于显式 true/false。
type Meta struct {
	Name       string `toml:"name"`
	Lang       string `toml:"lang"`        // go|python|rust|typescript
	Stage      Stage  `toml:"stage"`       // 空串 = 未标注
	SourceRead *bool  `toml:"source_read"` // nil = 未标注（三态）
	Usable     *bool  `toml:"usable"`
	Tested     *bool  `toml:"tested"`
	Summary    string `toml:"summary"`
	NextAction string `toml:"next_action"`
	NextDue    string `toml:"next_due"` // "2026-09-30"，空 = 未定
	Notes      string `toml:"notes"`
	UpdatedAt  string `toml:"updated_at"` // RFC3339 本地时区，由工具维护
}

// Validate 校验 stage 枚举与 next_due 日期格式，两者空值均放行。
func (m *Meta) Validate() error {
	if m.Stage != "" {
		valid := false
		for _, s := range ValidStages {
			if m.Stage == s {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("stage %q 非法，合法取值: %s", string(m.Stage), strings.Join(stageStrings(), " | "))
		}
	}
	if m.NextDue != "" {
		if _, err := time.Parse("2006-01-02", m.NextDue); err != nil {
			return fmt.Errorf("next_due %q 非法，须为 YYYY-MM-DD 格式", m.NextDue)
		}
	}
	return nil
}

// Load 读取 dir 下的 PROJECT.toml；文件不存在返回 (nil, nil)。
func Load(dir string) (*Meta, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Meta
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// Save 以固定字段顺序手写发射 PROJECT.toml。
// 注意：重写会丢弃用户手写注释（README 已明示）。写前自动刷新 UpdatedAt。
func Save(dir string, m *Meta) error {
	m.UpdatedAt = time.Now().Format(time.RFC3339)

	var b strings.Builder
	b.WriteString("# projstat 元数据 —— set 重写会丢弃手写注释\n")
	b.WriteString("# stage: idea | learning | wip | mvp | usable | paused | dropped\n")
	fmt.Fprintf(&b, "name = \"%s\"\n", escTOML(m.Name))
	fmt.Fprintf(&b, "lang = \"%s\"\n", escTOML(m.Lang))
	fmt.Fprintf(&b, "stage = \"%s\"\n", escTOML(string(m.Stage)))
	if m.SourceRead != nil {
		fmt.Fprintf(&b, "source_read = %t\n", *m.SourceRead)
	}
	if m.Usable != nil {
		fmt.Fprintf(&b, "usable = %t\n", *m.Usable)
	}
	if m.Tested != nil {
		fmt.Fprintf(&b, "tested = %t\n", *m.Tested)
	}
	fmt.Fprintf(&b, "summary = \"%s\"\n", escTOML(m.Summary))
	fmt.Fprintf(&b, "next_action = \"%s\"\n", escTOML(m.NextAction))
	fmt.Fprintf(&b, "next_due = \"%s\"\n", escTOML(m.NextDue))
	fmt.Fprintf(&b, "notes = \"%s\"\n", escTOML(m.Notes))
	fmt.Fprintf(&b, "updated_at = \"%s\"\n", escTOML(m.UpdatedAt))

	path := filepath.Join(dir, FileName)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// Skeleton 生成仅含 name/lang 的骨架元数据，其余字段零值。
func Skeleton(name, lang string) *Meta {
	return &Meta{Name: name, Lang: lang}
}

// escTOML 转义 TOML 基本字符串中的特殊字符：\ " 与 \n \r \t 控制字符，其余原样。
func escTOML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stageStrings() []string {
	out := make([]string, len(ValidStages))
	for i, s := range ValidStages {
		out[i] = string(s)
	}
	return out
}
