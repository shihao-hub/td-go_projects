package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"language_projects/go_projects/acp-title-proxy/internal/config"
)

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: sh-pocock-workflow
description: |
  Matt Pocock 软件工程全域工作流：源自《软件基本功没死，它在 AI 时代变得更值钱了》工程方法论。
  覆盖从大雾决策寻路到深模块架构重构演进。
version: 1.0.0
---

# Title
Some body text
`
	name, desc := ParseFrontmatter(content)
	if name != "sh-pocock-workflow" {
		t.Fatalf("expected name 'sh-pocock-workflow', got '%s'", name)
	}
	if !strings.Contains(desc, "Matt Pocock") || !strings.Contains(desc, "大雾决策寻路") {
		t.Fatalf("unexpected desc: %s", desc)
	}
}

func TestManagerScanAndRewrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "skill-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	skillDir := filepath.Join(tempDir, "my-test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	skillMd := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: my-test-skill
description: 这是一个测试技能
---
# Guide
`
	if err := os.WriteFile(skillMd, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.SkillsConfig{
		Enabled:          true,
		Prefix:           "skill:",
		Dirs:             []string{tempDir},
		AutoInjectPrompt: true,
	}

	mgr := NewManager(cfg)
	commands := mgr.GetCommandItems()
	if len(commands) < 2 { // "skill" 与 "skill:my-test-skill" 两条命令
		t.Fatalf("expected at least 2 commands, got %d", len(commands))
	}

	found := false
	for _, c := range commands {
		if c.Name == "skill:my-test-skill" {
			found = true
			if c.Description != "这是一个测试技能" {
				t.Fatalf("unexpected description: %s", c.Description)
			}
		}
	}
	if !found {
		t.Fatal("skill:my-test-skill command not found in list")
	}

	// 测试 MatchAndRewrite 处理带前缀命令 "/skill:my-test-skill"
	matched, rewritten, task, s := mgr.MatchAndRewrite("/skill:my-test-skill 请帮我测试")
	if !matched {
		t.Fatal("expected match for /skill:my-test-skill")
	}
	if task != "请帮我测试" {
		t.Fatalf("expected task '请帮我测试', got '%s'", task)
	}
	if s.Name != "my-test-skill" {
		t.Fatalf("expected skill name 'my-test-skill', got '%s'", s.Name)
	}
	if !strings.Contains(rewritten, "【系统注入：激活技能 my-test-skill】") {
		t.Fatalf("expected prompt to contain injection header, got:\n%s", rewritten)
	}

	// 测试 MatchAndRewrite 处理无前缀命令 "/my-test-skill"（双向兼容）
	matchedDirect, _, _, _ := mgr.MatchAndRewrite("/my-test-skill 请帮我测试")
	if !matchedDirect {
		t.Fatal("expected fallback match for /my-test-skill")
	}

	// 测试 MatchAndRewrite 处理通用 /skill 命令
	matchedGen, rewrittenGen, taskGen, sGen := mgr.MatchAndRewrite("/skill my-test-skill 进行验证")
	if !matchedGen {
		t.Fatal("expected match for /skill my-test-skill")
	}
	if taskGen != "进行验证" {
		t.Fatalf("expected task '进行验证', got '%s'", taskGen)
	}
	if sGen.Name != "my-test-skill" {
		t.Fatalf("expected skill name 'my-test-skill', got '%s'", sGen.Name)
	}
	if !strings.Contains(rewrittenGen, "【系统注入：激活技能 my-test-skill】") {
		t.Fatalf("expected prompt to contain injection header, got:\n%s", rewrittenGen)
	}

	// 测试非技能命令（如 /plan）
	matchedPlan, _, _, _ := mgr.MatchAndRewrite("/plan 仔细规划")
	if matchedPlan {
		t.Fatal("should NOT match /plan")
	}
}
