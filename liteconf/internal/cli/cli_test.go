package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shihao-hub/liteconf/internal/version"
)

// testOptions 构造内存标准流与必失败 LookPath 的运行依赖，
// 供分发类用例使用（不应触发 LookPath 的用例若触发即失败）。
func testOptions(stdout, stderr *bytes.Buffer) Options {
	return Options{
		Stdin:    strings.NewReader(""),
		Stdout:   stdout,
		Stderr:   stderr,
		LookPath: func(string) (string, error) { return "", errors.New("不应触发 LookPath") },
	}
}

func TestUnknownSubcommand(t *testing.T) {
	for _, args := range [][]string{{"foo"}, {"--unknown"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, testOptions(&stdout, &stderr)); code != 2 {
			t.Errorf("Run(%v) 退出码 = %d，want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("Run(%v) stdout 应为空，得到 %q", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "未知子命令") {
			t.Errorf("Run(%v) stderr 应含人读错误，得到 %q", args, stderr.String())
		}
	}
}

func TestHelpVariants(t *testing.T) {
	for _, args := range [][]string{{}, {"-h"}, {"--help"}, {"help"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, testOptions(&stdout, &stderr)); code != 0 {
			t.Errorf("Run(%v) 退出码 = %d，want 0", args, code)
		}
		out := stdout.String()
		if !strings.Contains(out, "http") || !strings.Contains(out, "schema") {
			t.Errorf("Run(%v) 帮助应含 http 与 schema，得到 %q", args, out)
		}
		if stderr.Len() != 0 {
			t.Errorf("Run(%v) stderr 应为空，得到 %q", args, stderr.String())
		}
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, testOptions(&stdout, &stderr)); code != 0 {
			t.Fatalf("Run(%v) 退出码 = %d，want 0", args, code)
		}
		if got := strings.TrimSpace(stdout.String()); got != version.Version {
			t.Errorf("Run(%v) 输出 %q，want %q", args, got, version.Version)
		}
	}
}

func TestSchema(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"schema"}, testOptions(&stdout, &stderr)); code != 0 {
		t.Fatalf("schema 退出码 = %d，want 0", code)
	}
	var parsed struct {
		Name      string `json:"name"`
		Interface string `json:"interface"`
		Commands  []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("schema 输出不是合法 JSON：%v\n%s", err, stdout.String())
	}
	if parsed.Interface != "cli" {
		t.Errorf("interface = %q，want \"cli\"", parsed.Interface)
	}
	var hasHTTP bool
	for _, c := range parsed.Commands {
		if c.Name == "http" {
			hasHTTP = true
		}
	}
	if !hasHTTP {
		t.Error("schema commands 应包含 http")
	}
	if stderr.Len() != 0 {
		t.Errorf("schema stderr 应为空，得到 %q", stderr.String())
	}
}

func TestHTTPCurlieMissing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opt := testOptions(&stdout, &stderr)
	opt.LookPath = func(string) (string, error) {
		return "", errors.New(`exec: "curlie": executable file not found in %PATH%`)
	}
	args := []string{"http", "GET", "localhost:8646/api/app1/dev"}
	if code := Run(args, opt); code != 1 {
		t.Errorf("curlie 缺失退出码 = %d，want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("curlie 缺失时 stdout 应为空，得到 %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "go install github.com/rs/curlie@latest") {
		t.Errorf("stderr 应含安装指引，得到 %q", stderr.String())
	}
}
