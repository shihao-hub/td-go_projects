package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSysTagLabelAndColor(t *testing.T) {
	for _, def := range SysTagDefs {
		if got := SysTagLabel(def.Key); got != def.Label {
			t.Errorf("SysTagLabel(%q) = %q, want %q", def.Key, got, def.Label)
		}
		if c, ok := SysTagColor(def.Key); !ok || c != def.Color {
			t.Errorf("SysTagColor(%q) = (%d,%v), want (%d,true)", def.Key, c, ok, def.Color)
		}
	}
	if got := SysTagLabel("no-such-tag"); got != "" {
		t.Errorf("未知 key 应返回空串, got %q", got)
	}
	if _, ok := SysTagColor("no-such-tag"); ok {
		t.Error("未知 key 不应有颜色")
	}
}

func TestSanitizeSysTag(t *testing.T) {
	for _, def := range SysTagDefs {
		if got := SanitizeSysTag(def.Key); got != def.Key {
			t.Errorf("SanitizeSysTag(%q) = %q, want 原值", def.Key, got)
		}
	}
	for _, bad := range []string{"", "TODO", "todo ", "unknown"} {
		if got := SanitizeSysTag(bad); got != SysTagNone {
			t.Errorf("SanitizeSysTag(%q) = %q, want 空串", bad, got)
		}
	}
}

func TestTagColumnText(t *testing.T) {
	cases := []struct {
		name            string
		sys, usr        string
		wantContains    []string
		wantNotContains []string
	}{
		{"双标签", "todo", "常用小工具", []string{"●", "待完善", "常用小工具"}, nil},
		{"仅系统", "broken", "", []string{"●", "有问题"}, nil},
		{"仅用户", "", "临时脚本", []string{"临时脚本"}, []string{"●"}},
		{"都无", "", "", nil, []string{"●"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := (&Entry{SysTag: c.sys, UserTag: c.usr}).TagColumnText()
			for _, s := range c.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("TagColumnText() = %q, 应包含 %q", got, s)
				}
			}
			for _, s := range c.wantNotContains {
				if strings.Contains(got, s) {
					t.Errorf("TagColumnText() = %q, 不应包含 %q", got, s)
				}
			}
		})
	}
}

func TestEntryTagsJSONRoundTrip(t *testing.T) {
	e := Entry{
		Name: "x", Path: `C:\x.exe`, AddedAt: "2026-01-01T00:00:00Z",
		SysTag: "todo", UserTag: "描述文本",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var back Entry
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.SysTag != "todo" || back.UserTag != "描述文本" {
		t.Errorf("标签往返不一致: %+v", back)
	}
}

func TestEntryOldJSONNoTags(t *testing.T) {
	// 旧版 config.json 没有标签字段，反序列化应为空（向后兼容）
	old := `{"name":"a","path":"C:\\a.exe","added_at":"2026-01-01T00:00:00Z"}`
	var e Entry
	if err := json.Unmarshal([]byte(old), &e); err != nil {
		t.Fatal(err)
	}
	if e.SysTag != "" || e.UserTag != "" {
		t.Errorf("旧格式应无标签: %+v", e)
	}
}

func TestNewStoreSanitizesTags(t *testing.T) {
	s := NewStore([]Entry{
		{Path: `C:\a.exe`, SysTag: "hacked", UserTag: "  描述  "},
		{Path: `C:\b.exe`, SysTag: "stable"},
	})
	if s.Entries[0].SysTag != SysTagNone {
		t.Errorf("未知系统标签应清洗为空: %q", s.Entries[0].SysTag)
	}
	if s.Entries[0].UserTag != "描述" {
		t.Errorf("用户标签应去首尾空白: %q", s.Entries[0].UserTag)
	}
	if s.Entries[1].SysTag != "stable" {
		t.Errorf("合法系统标签应保留: %q", s.Entries[1].SysTag)
	}
}

func TestCountBySysTag(t *testing.T) {
	s := NewStore([]Entry{
		{Path: `C:\a.exe`, SysTag: "todo"},
		{Path: `C:\b.exe`, SysTag: "todo"},
		{Path: `C:\c.exe`, SysTag: "stable"},
		{Path: `C:\d.exe`},
	})
	if n := s.CountBySysTag("todo"); n != 2 {
		t.Errorf("CountBySysTag(todo) = %d, want 2", n)
	}
	if n := s.CountBySysTag(SysTagNone); n != 1 {
		t.Errorf("CountBySysTag(空) = %d, want 1", n)
	}
	if n := s.CountBySysTag("verify"); n != 0 {
		t.Errorf("CountBySysTag(verify) = %d, want 0", n)
	}
}
