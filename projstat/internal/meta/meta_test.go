package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateStage(t *testing.T) {
	// 空串 = 未标注，放行
	if err := (&Meta{}).Validate(); err != nil {
		t.Fatalf("空 Meta 应校验通过， got %v", err)
	}
	for _, s := range ValidStages {
		if err := (&Meta{Stage: s}).Validate(); err != nil {
			t.Errorf("stage=%s 应合法，got %v", s, err)
		}
	}
	err := (&Meta{Stage: "archived"}).Validate()
	if err == nil {
		t.Fatal("stage=archived 应被拒绝")
	}
	if !strings.Contains(err.Error(), "idea") {
		t.Errorf("错误信息应列出合法取值，got %q", err.Error())
	}
}

func TestValidateDue(t *testing.T) {
	cases := map[string]bool{
		"":           true,
		"2026-09-30": true,
		"2026-9-30":  false, // 非零填充
		"2026-13-99": false,
		"20260930":   false,
		"2026/09/30": false,
	}
	for due, ok := range cases {
		err := (&Meta{NextDue: due}).Validate()
		if ok && err != nil {
			t.Errorf("next_due=%q 应合法，got %v", due, err)
		}
		if !ok && err == nil {
			t.Errorf("next_due=%q 应被拒绝", due)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil || m != nil {
		t.Fatalf("文件不存在应返回 (nil,nil)，got (%v,%v)", m, err)
	}
}

func TestLoadParseError(t *testing.T) {
	dir := t.TempDir()
	bad := "name = \"unclosed"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("解析失败应报错")
	}
	if !strings.Contains(err.Error(), FileName) {
		t.Errorf("错误信息应包含文件路径，got %q", err.Error())
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t1, t2, f1 := true, false, true
	m := &Meta{
		Name:       "zedhub",
		Lang:       "python",
		Stage:      "usable",
		SourceRead: &t1,
		Usable:     &t2,
		Tested:     &f1,
		Summary:    "Zed 会话 SQLite 只读查询 CLI",
		NextAction: "补导出过滤参数",
		NextDue:    "2026-09-30",
		Notes: `含 \ 反斜杠 "引号" 与
换行	tab`,
	}
	if err := Save(dir, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != m.Name || got.Lang != m.Lang || got.Stage != m.Stage {
		t.Errorf("基础字段往返不一致: %+v", got)
	}
	if got.SourceRead == nil || *got.SourceRead != true {
		t.Errorf("source_read 往返应保持 true")
	}
	if got.Usable == nil || *got.Usable != false {
		t.Errorf("usable 往返应保持显式 false（三态区分）")
	}
	if got.Tested == nil || *got.Tested != true {
		t.Errorf("tested 往返应保持 true")
	}
	if got.Summary != m.Summary || got.NextAction != m.NextAction || got.NextDue != m.NextDue {
		t.Errorf("文本字段往返不一致: %+v", got)
	}
	if got.Notes != m.Notes {
		t.Errorf("Notes 转义往返不一致:\nwant %q\ngot  %q", m.Notes, got.Notes)
	}
	if _, err := time.Parse(time.RFC3339, got.UpdatedAt); err != nil {
		t.Errorf("updated_at 应为 RFC3339，got %q", got.UpdatedAt)
	}
}

func TestSaveSkeletonOmitsBools(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Skeleton("demo", "go")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, line := range strings.Split(s, "\n") {
		for _, key := range []string{"source_read", "usable", "tested"} {
			if strings.HasPrefix(line, key) {
				t.Errorf("骨架文件不应含行首字段 %q（三态未标注省略输出），got:\n%s", key, s)
			}
		}
	}
	if !strings.Contains(s, `name = "demo"`) || !strings.Contains(s, `lang = "go"`) {
		t.Errorf("骨架文件应含 name/lang，got:\n%s", s)
	}
}

func TestEscTOML(t *testing.T) {
	cases := map[string]string{
		`a"b`:     `a\"b`,
		`a\b`:     `a\\b`,
		"a\nb":    `a\nb`,
		"a\rb":    `a\rb`,
		"a\tb":    `a\tb`,
		"中文 保持原样": "中文 保持原样",
	}
	for in, want := range cases {
		if got := escTOML(in); got != want {
			t.Errorf("escTOML(%q) = %q, want %q", in, got, want)
		}
	}
}
