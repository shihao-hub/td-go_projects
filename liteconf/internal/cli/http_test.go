package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// fakeCurlieEnv 标记测试二进制以"假 curlie"模式运行（仅用于自举测试）。
const fakeCurlieEnv = "LITECONF_TEST_FAKE_CURLIE"

// TestHelperFakeCurlie 不是常规用例：父用例以测试二进制自身为"curlie"启动
// 本进程（env 标记打开），把 -- 之后的透传参数回显 stdout、原样转发 stdin，
// 并以固定退出码 7 结束，供父用例断言透传语义。
func TestHelperFakeCurlie(t *testing.T) {
	if os.Getenv(fakeCurlieEnv) != "1" {
		return
	}
	for i, a := range os.Args {
		if a == "--" {
			if _, err := fmt.Fprintf(os.Stdout, "%s\n", strings.Join(os.Args[i+1:], "\x1f")); err != nil {
				os.Exit(2)
			}
			break
		}
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		os.Exit(3)
	}
	os.Exit(7)
}

// TestHTTPPassthroughVerbatim 验证透传编排：
// 参数逐字透传、curlie 退出码透传、stdin 直通子进程。
func TestHTTPPassthroughVerbatim(t *testing.T) {
	t.Setenv(fakeCurlieEnv, "1")
	var stdout, stderr bytes.Buffer
	opt := Options{
		Stdin:    strings.NewReader("hello-from-stdin"),
		Stdout:   &stdout,
		Stderr:   &stderr,
		LookPath: func(string) (string, error) { return os.Args[0], nil },
	}
	want := strings.Join([]string{"GET", "localhost:8646/api/app1/dev", "--print", "--help", "a b"}, "\x1f")
	args := []string{"http", "-test.run=TestHelperFakeCurlie", "--", "GET", "localhost:8646/api/app1/dev", "--print", "--help", "a b"}
	if code := Run(args, opt); code != 7 {
		t.Fatalf("透传退出码 = %d，want 7；stderr: %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("参数未逐字透传\nwant 子串: %q\ngot: %q", want, stdout.String())
	}
	if !strings.Contains(stdout.String(), "hello-from-stdin") {
		t.Error("stdin 未直通到子进程")
	}
}
