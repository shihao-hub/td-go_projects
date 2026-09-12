package ignore

import (
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type rule struct {
	pattern string
	negated bool
	dirOnly bool
}

type Matcher struct {
	rules []rule
}

func NewMatcher(rules []string) *Matcher {
	m := &Matcher{}
	for _, raw := range rules {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimSpace(line[1:])
		}
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		if !strings.Contains(line, "/") {
			line = "**/" + line
		}
		m.rules = append(m.rules, rule{pattern: line, negated: negated, dirOnly: dirOnly})
	}
	return m
}

func (m *Matcher) Match(p string, isDir bool) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return false
	}
	matched := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		ok, err := doublestar.Match(r.pattern, p)
		if err != nil {
			continue
		}
		if ok {
			matched = !r.negated
		}
	}
	return matched
}
