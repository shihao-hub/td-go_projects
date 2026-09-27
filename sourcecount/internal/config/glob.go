package config

import (
	"fmt"
	"path"
	"strings"
)

type pattern struct {
	raw      string
	segments []string
}

// Matcher 实现 include/exclude 的路径匹配。路径必须是相对根目录的 / 路径。
type Matcher struct {
	includes []pattern
	excludes []pattern
}

// NewMatcher 创建并校验 Glob matcher。
func NewMatcher(includes, excludes []string) (Matcher, error) {
	m := Matcher{}
	for _, raw := range includes {
		p, err := NewPattern(raw)
		if err != nil {
			return Matcher{}, err
		}
		m.includes = append(m.includes, p)
	}
	for _, raw := range excludes {
		p, err := NewPattern(raw)
		if err != nil {
			return Matcher{}, err
		}
		m.excludes = append(m.excludes, p)
	}
	return m, nil
}

// NewPattern 校验单个 Glob。** 只能作为完整路径段出现。
func NewPattern(raw string) (pattern, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	raw = strings.TrimPrefix(raw, "./")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return pattern{}, fmt.Errorf("Glob 不能为空")
	}
	segments := strings.Split(raw, "/")
	for _, segment := range segments {
		if segment == "" {
			return pattern{}, fmt.Errorf("Glob 含有空路径段: %q", raw)
		}
		if strings.Contains(segment, "**") && segment != "**" {
			return pattern{}, fmt.Errorf("Glob 中的 ** 必须独占一个路径段: %q", raw)
		}
		if segment != "**" {
			if _, err := path.Match(segment, "probe"); err != nil {
				return pattern{}, fmt.Errorf("Glob 无效 %q: %w", raw, err)
			}
		}
	}
	return pattern{raw: raw, segments: segments}, nil
}

// IncludeDir 判断目录是否存在可能命中的 include 后代。
func (m Matcher) IncludeDir(rel string) bool {
	if len(m.includes) == 0 {
		return true
	}
	segments := splitPath(rel)
	for _, p := range m.includes {
		if p.canMatchPrefix(segments) {
			return true
		}
	}
	return false
}

// IncludeFile 判断文件是否被 include 保留。目录精确命中时，其后代也保留。
func (m Matcher) IncludeFile(rel string) bool {
	if len(m.includes) == 0 {
		return true
	}
	segments := splitPath(rel)
	for _, p := range m.includes {
		if p.matches(segments) {
			return true
		}
		for i := 1; i < len(segments); i++ {
			if p.matches(segments[:i]) {
				return true
			}
		}
	}
	return false
}

// ExcludeDir 判断目录本身是否命中排除规则，命中后可剪枝整棵子树。
func (m Matcher) ExcludeDir(rel string) bool {
	segments := splitPath(rel)
	for _, p := range m.excludes {
		if p.matches(segments) {
			return true
		}
	}
	return false
}

// ExcludeFile 判断文件或其祖先目录是否命中排除规则。
func (m Matcher) ExcludeFile(rel string) bool {
	segments := splitPath(rel)
	for _, p := range m.excludes {
		if p.matches(segments) {
			return true
		}
		for i := 1; i < len(segments); i++ {
			if p.matches(segments[:i]) {
				return true
			}
		}
	}
	return false
}

func (p pattern) matches(segments []string) bool {
	memo := make(map[[2]int]bool)
	seen := make(map[[2]int]bool)
	var match func(int, int) bool
	match = func(pi, si int) bool {
		key := [2]int{pi, si}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var result bool
		switch {
		case pi == len(p.segments):
			result = si == len(segments)
		case p.segments[pi] == "**":
			result = match(pi+1, si) || (si < len(segments) && match(pi, si+1))
		case si < len(segments):
			ok, err := path.Match(p.segments[pi], segments[si])
			result = err == nil && ok && match(pi+1, si+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}

// canMatchPrefix 判断 pattern 是否可能匹配以 segments 为前缀的某个路径。
func (p pattern) canMatchPrefix(segments []string) bool {
	var match func(int, int) bool
	match = func(pi, si int) bool {
		if si == len(segments) {
			return true
		}
		if pi == len(p.segments) {
			return false
		}
		if p.segments[pi] == "**" {
			return match(pi+1, si) || match(pi, si+1)
		}
		ok, err := path.Match(p.segments[pi], segments[si])
		return err == nil && ok && match(pi+1, si+1)
	}
	return match(0, 0)
}

func splitPath(value string) []string {
	value = strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}
