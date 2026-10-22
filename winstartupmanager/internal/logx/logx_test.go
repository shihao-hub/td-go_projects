package logx

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesFile(t *testing.T) {
	dir := t.TempDir()
	logger := Init(dir)
	Infof("启动测试 %s", "ok")
	Close()

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("日志文件未生成: %v", err)
	}
	if !strings.Contains(string(data), "启动测试 ok") {
		t.Fatalf("日志内容缺失: %s", data)
	}
	if logger == nil {
		t.Fatal("Init 必须返回 logger")
	}
}

func TestInitIsIdempotent(t *testing.T) {
	Close()
	dir := t.TempDir()
	first := Init(dir)
	second := Init(dir)
	Close()
	if first != second {
		t.Fatal("重复 Init 必须返回同一 logger")
	}
}

func TestRotateOverLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 200)), 0644); err != nil {
		t.Fatal(err)
	}

	if err := rotate(path, 100); err != nil {
		t.Fatalf("轮转失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("轮转后原文件应被移走")
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("轮转后应存在 .1 文件: %v", err)
	}
}

func TestRotateKeepsSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("small"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rotate(path, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("未超限不应轮转")
	}
}

func TestTeeWriterSurvivesBrokenStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := teeWriter{stderr: failingWriter{}, file: f}
	if _, err := w.Write([]byte("落盘优先")); err != nil {
		t.Fatalf("stderr 失败不应影响文件写入: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "落盘优先" {
		t.Fatalf("文件内容错误: %q", data)
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("stderr unavailable")
}

func TestSystemLoggerDropsRequestNoise(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(&filterHandler{
		inner: slog.NewTextHandler(&buf, nil),
		drop:  systemNoisePrefixes,
	})

	logger.Info("[AssetFileServerFS] Handling request", "url", "/")
	logger.Info("保留这条")

	out := buf.String()
	if strings.Contains(out, "Handling request") {
		t.Fatalf("噪音日志未被过滤: %s", out)
	}
	if !strings.Contains(out, "保留这条") {
		t.Fatalf("正常日志被误删: %s", out)
	}
}
