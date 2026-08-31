package main

import (
	"strings"
	"testing"
)

func TestRtfEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`a\b`, `a\\b`},
		{`{x}`, `\{x\}`},
		{"中", `\u20013?`},          // U+4E2D
		{"😀", `\u-10179?\u-8704?`}, // U+1F600 代理对（有符号 int16）
		{"abc", "abc"},
	}
	for _, c := range cases {
		if got := rtfEscape(c.in); got != c.want {
			t.Errorf("rtfEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInlineRTF(t *testing.T) {
	cases := []struct {
		name, in string
		contains []string
	}{
		{"粗体", `a**b**c`, []string{`\b b\b0`}},
		{"斜体", `a*i*b`, []string{`\i i\i0`}},
		{"行内代码", "a`b`c", []string{`\f1\fs19\highlight3 b\highlight0\f0\fs20`}},
		{"链接", `[text](https://x)`, []string{`\cf2 text`, `\cf4 (https://x)\cf0`}},
		{"链接同文本", `[a](a)`, []string{`\cf2 a\cf0`}},
		{"未闭合粗体原样", `a**b`, []string{`a**b`}},
		{"混合", `**b** and *i*`, []string{`\b b\b0`, `\i i\i0`}},
	}
	for _, c := range cases {
		got := inlineRTF(c.in)
		for _, want := range c.contains {
			if !strings.Contains(got, want) {
				t.Errorf("%s: inlineRTF(%q) = %q, 缺少 %q", c.name, c.in, got, want)
			}
		}
	}
}

func TestMdToRTFBlocks(t *testing.T) {
	cases := []struct {
		name, line string
		contains   []string
	}{
		{"h1", "# Title", []string{`\fs36\b `, `\b0\fs20\par`}},
		{"h3", "### Sub", []string{`\fs28\b `}},
		{"h6", "###### Six", []string{`\fs24\b `}},
		{"hr", "---", []string{`\brdrb\brdrs\brdrw10`}},
		{"引用", "> quoted", []string{`\li284\cf4\i `, `\i0\cf0\li0\par`}},
		{"无序列表", "- item", []string{`\fi-284\li284 \u8226?  item\par`}},
		{"有序列表", "12. item", []string{`\fi-340\li340 12. item\par`}},
		{"段落", "plain para", []string{`\pard\fs20 plain para\par`}},
	}
	for _, c := range cases {
		got := mdToRTF(c.line + "\n")
		for _, want := range c.contains {
			if !strings.Contains(got, want) {
				t.Errorf("%s: mdToRTF(%q) = %q, 缺少 %q", c.name, c.line, got, want)
			}
		}
	}
}

func TestMdToRTFCodeBlock(t *testing.T) {
	in := "before\n```\nfmt.Println(42)\n```\nafter"
	got := mdToRTF(in)
	for _, want := range []string{
		`\f1\fs19\highlight3 fmt.Println(42)\highlight0\f0\fs20\par`,
		`\pard\fs20 before\par`,
		`\pard\fs20 after\par`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("代码块渲染缺少 %q\n输出: %s", want, got)
		}
	}
}

func TestMdToRTFDocument(t *testing.T) {
	in := "# Tool\n\nOne line intro.\n\n## Usage\n\n- `-list` list only\n- `-yes` skip confirm\n\n```\nexe -install\n```\n\n> Note: quoted text\n\n---\n\nTail **bold** end.\n"
	got := mdToRTF(in)
	for _, want := range []string{
		`{\rtf1\ansi\deff0`,
		`\fs36\b `,
		`\b0\fs20\par`,
		`\b bold\b0`,
		`\brdrb`,
		`\u8226?`,
		`exe -install`,
		`\cf4\i`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("整篇渲染缺少 %q", want)
		}
	}
	if !strings.HasSuffix(got, "}") {
		t.Error("RTF 未正确闭合")
	}
}
