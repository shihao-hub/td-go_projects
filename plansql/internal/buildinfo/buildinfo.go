package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

var (
	// Version 是展示的 git describe 结果，由构建注入或回退 VCS
	Version = ""
	// BuildID 是握手与升级检测凭据
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

// 生产构建正则：形如 v1.0.0 或 plansql/v1.0.0
var productionRe = regexp.MustCompile(`^(?:plansql/)?v\d+\.\d+\.\d+$`)

// IsProduction 判断当前二进制是否为生产构建（干净的 release tag）
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
