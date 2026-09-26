// Package buildinfo 是构建指纹（Version 与 BuildID）的单点来源。
// 构建脚本经 -ldflags -X 注入；未注入时回退到 Go 编译期嵌入的 VCS 信息，
// 保证 `go run` / 测试构建也能得到稳定的开发指纹。
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

var (
	// Version 是 --version 展示的 git describe 结果，由构建脚本注入。
	Version = ""
	// BuildID 是握手与升级检测凭据（describe [+ .时间戳]），由构建脚本注入。
	BuildID = ""
)

func init() {
	if Version != "" && BuildID != "" {
		return
	}
	v := vcsVersion()
	if Version == "" {
		Version = v
	}
	if BuildID == "" {
		BuildID = v
	}
}

// vcsVersion 从编译期嵌入的 VCS 信息生成 "短hash[-dirty]"，取不到返回 "dev"。
func vcsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	short := revision
	if len(short) > 7 {
		short = short[:7]
	}
	if modified {
		return short + "-dirty"
	}
	return short
}

// productionRe 是干净 tag 形态：`vX.Y.Z`，允许 monorepo 项目前缀 `agyquota/`。
var productionRe = regexp.MustCompile(`^(?:agyquota/)?v\d+\.\d+\.\d+$`)

// IsProduction 判断当前构建是否为生产构建：
// 只解析 BuildID——仅当末尾 "." 分隔段为 ≥9 位纯十进制数（Unix 秒时间戳）
// 时才剥离，剩余部分匹配干净 tag 形态即为生产；Version 仅供展示。
func IsProduction() bool {
	id := BuildID
	if id == "" {
		return false
	}
	if i := strings.LastIndex(id, "."); i >= 0 {
		tail := id[i+1:]
		if len(tail) >= 9 && isDigits(tail) {
			id = id[:i]
		}
	}
	return productionRe.MatchString(id)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
