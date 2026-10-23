// logx_test.go：日志双写、轮转与 stderr 故障容错。
package logx

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stderr closed") }

func TestTeeWriter_IgnoresStderrFailure(t *testing.T) {
	var file bytes.Buffer
	writer := teeWriter{stderr: failingWriter{}, file: &file}

	n, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 || file.String() != "hello" {
		t.Fatalf("n=%d file=%q", n, file.String())
	}
}

func TestInit_WritesFile(t *testing.T) {
	dir := t.TempDir()
	logger := Init(dir)
	logger.Info("termhub started", "channel", "dev")
	Close()

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "termhub started") || !strings.Contains(string(data), "channel=dev") {
		t.Fatalf("log = %q", data)
	}
}

func TestRotate_KeepsOneHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	rotate(path)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("history missing: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("current log should be moved, err=%v", err)
	}
}
