package parser

import (
	"strings"
	"testing"
)

const legacyNote = "---\n" +
	"name: kb-git-rebase-permission-denied\n" +
	"description: 报错 `logs/HEAD: Permission denied` 的根因：独占句柄锁与安全抢救。\n" +
	"---\n" +
	"\n" +
	"# 标题\n" +
	"\n" +
	"正文内容。\n"

func TestParseToleratesUnquotedColonInDescription(t *testing.T) {
	doc, err := Parse([]byte(legacyNote))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Name != "kb-git-rebase-permission-denied" {
		t.Fatalf("unexpected name: %q", doc.Meta.Name)
	}
	if !strings.Contains(doc.Meta.Description, "Permission denied") {
		t.Fatalf("unexpected description: %q", doc.Meta.Description)
	}
}

func TestRenderKeepsOriginalLinesAndAppendsOnly(t *testing.T) {
	doc, err := Parse([]byte(legacyNote))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	doc.SetScalar("id", "002")
	doc.SetScalar("last_verified", "2026-10-20")
	doc.SetBlock("invocation", []string{"invocation:", "  auto_trigger: true"})

	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	rendered := string(out)
	for _, line := range strings.Split(legacyNote, "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(rendered, line) {
			t.Fatalf("original line lost: %q", line)
		}
	}

	if !strings.Contains(rendered, "id: \"002\"") {
		t.Fatalf("missing injected id:\n%s", rendered)
	}
	if !strings.Contains(rendered, "\n# 标题\n") {
		t.Fatalf("body was not preserved verbatim:\n%s", rendered)
	}

	if _, err := Parse(out); err != nil {
		t.Fatalf("rendered note is not parseable again: %v", err)
	}
}

func TestSetScalarReplacesInPlace(t *testing.T) {
	note := "---\nstatus: verified\nlast_verified: \"2026-01-01\"\n---\n\nbody\n"
	doc, err := Parse([]byte(note))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	doc.SetScalar("status", "suspect")
	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	rendered := string(out)
	if strings.Contains(rendered, "status: verified") {
		t.Fatalf("old status was not replaced:\n%s", rendered)
	}
	if !strings.Contains(rendered, "status: suspect") {
		t.Fatalf("new status missing:\n%s", rendered)
	}
	if strings.Count(rendered, "last_verified:") != 1 {
		t.Fatalf("last_verified duplicated:\n%s", rendered)
	}
}
