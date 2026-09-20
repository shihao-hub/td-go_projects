package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/shihao-hub/liteconf/internal/mcp"
	"github.com/shihao-hub/liteconf/internal/version"
)

func TestSchemaContract(t *testing.T) {
	var buf bytes.Buffer
	opt := Options{Stdout: &buf, Stderr: io.Discard}
	if code := runSchema(&opt); code != 0 {
		t.Fatalf("runSchema exit = %d", code)
	}

	var c contract
	if err := json.Unmarshal(buf.Bytes(), &c); err != nil {
		t.Fatalf("decode schema output: %v\noutput: %s", err, buf.String())
	}
	if c.Name != "liteconf" || c.Version == "" {
		t.Fatalf("name/version: %q %q", c.Name, c.Version)
	}

	// 同源对照：schema 导出的 tools 与 MCP 注册视图名称集合一致
	specs, err := mcp.ToolSpecs(context.Background(), mcp.Config{
		ServerURL: mcp.DefaultServerURL, Version: version.Version,
	})
	if err != nil {
		t.Fatalf("ToolSpecs: %v", err)
	}
	got := make([]string, 0, len(c.Tools))
	for _, tl := range c.Tools {
		got = append(got, tl.Name)
	}
	want := make([]string, 0, len(specs))
	for _, tl := range specs {
		want = append(want, tl.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools mismatch: schema=%v registry=%v", got, want)
	}

	// 每个 tool 的 JSON 形状含标准 5.5 要求的键
	for _, tl := range c.Tools {
		b, err := json.Marshal(tl)
		if err != nil {
			t.Fatalf("marshal tool %s: %v", tl.Name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("decode tool %s: %v", tl.Name, err)
		}
		for _, key := range []string{"name", "description", "inputSchema", "outputSchema", "annotations"} {
			if _, ok := m[key]; !ok {
				t.Errorf("tool %s json missing key %q", tl.Name, key)
			}
		}
	}

	// commands 含全部子命令条目
	cmds := map[string]bool{}
	for _, cmd := range c.Commands {
		cmds[cmd.Name] = true
	}
	for _, want := range []string{"http", "mcp", "schema", "version", "help"} {
		if !cmds[want] {
			t.Errorf("command %q missing", want)
		}
	}
}

func TestRunMCPParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"unknown flag", []string{"--bogus"}, 2},
		{"positional arg", []string{"extra"}, 2},
		{"bad scheme", []string{"-server", "ftp://example.com"}, 2},
	}
	for _, tc := range cases {
		opt := Options{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
		if got := runMCP(tc.args, &opt); got != tc.want {
			t.Errorf("%s: want exit %d, got %d", tc.name, tc.want, got)
		}
	}
}

func TestRunDispatchesMCP(t *testing.T) {
	// Run 分发 smoke：mcp 子命令能被路由（参数错误路径，不真正启动 server）
	opt := Options{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	if got := Run([]string{"mcp", "--no-such-flag"}, opt); got != 2 {
		t.Fatalf("Run mcp --no-such-flag: want exit 2, got %d", got)
	}
}
