package ignore

import "testing"

func TestMatch(t *testing.T) {
	m := NewMatcher([]string{
		"node_modules/",
		"*.log",
		"!keep.log",
		"**/temp/",
		"build",
		"docs/only.txt",
		"# comment",
		"",
	})

	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true},
		{"sub/node_modules", true, true},
		{"node_modules", false, false},
		{"a/x.log", false, true},
		{"x.log", false, true},
		{"keep.log", false, false},
		{"sub/keep.log", false, false},
		{"temp", true, true},
		{"a/temp", true, true},
		{"temp", false, false},
		{"build", true, true},
		{"src/build", false, true},
		{"docs/only.txt", false, true},
		{"other/only.txt", false, false},
		{"src/main.go", false, false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, c.isDir); got != c.want {
			t.Errorf("Match(%q, isDir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestLastRuleWins(t *testing.T) {
	m := NewMatcher([]string{"*.log", "!debug.log", "debug.log"})
	if !m.Match("debug.log", false) {
		t.Error("debug.log should be ignored when rule appears after negation")
	}
}
