package runner

import (
	"reflect"
	"testing"
)

func TestSuggest(t *testing.T) {
	names := []string{"go", "gofmt", "gopls", "git", "gcc", "python", "cargo"}

	cases := []struct {
		name  string
		input string
		limit int
		want  []string
	}{
		{"前缀匹配按字典序取前3", "g", 3, []string{"gcc", "git", "go"}},
		{"前缀优先于子串", "go", 3, []string{"gofmt", "gopls", "cargo"}},
		{"短输入只做前缀（无编辑距离）", "gi", 3, []string{"git"}},
		{"子串包含", "yth", 3, []string{"python"}},
		{"短输入不做编辑距离", "gq", 3, nil},
		{"编辑距离 len<6 阈值1", "pythn", 3, []string{"python"}},
		{"编辑距离 len>=6 阈值2", "pythoo", 3, []string{"python"}},
		{"距离超阈值不入选", "gxxxx", 3, nil},
		{"无任何匹配", "zzzzzzz", 3, nil},
		{"空输入", "", 3, nil},
		{"limit 截断", "go", 1, []string{"gofmt"}},
		{"与注册名完全一致", "python", 3, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := suggest(c.input, names, c.limit)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("suggest(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"pythn", "python", 1},
		{"中文测试", "中文加油", 2},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
