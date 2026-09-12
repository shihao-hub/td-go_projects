package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	lock "instancelock"
)

// setupTest 注入输出缓冲并复位全局状态，返回捕获 stdout 的 buf
func setupTest(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	stdoutWriter = &buf
	Pretty = false
	t.Cleanup(func() {
		stdoutWriter = os.Stdout
		Pretty = false
	})
	return &buf
}

// randKey 生成测试用唯一 key，并注册锁文件清理
func randKey(t *testing.T) string {
	t.Helper()
	key := fmt.Sprintf("test.instancelock.%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(lock.PathOf(key)) })
	return key
}

// decode 断言输出为合法 JSON 并解析为 map
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n输出: %s", err, buf.String())
	}
	return m
}

func data(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	if m["ok"] != true {
		t.Fatalf("期望 ok=true，实际: %v", m)
	}
	d, _ := m["data"].(map[string]any)
	return d
}

func errCode(t *testing.T, m map[string]any) string {
	t.Helper()
	if m["ok"] != false {
		t.Fatalf("期望 ok=false，实际: %v", m)
	}
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestVersion(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"version"}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	if d["name"] != "instancelock" {
		t.Errorf("name = %v", d["name"])
	}
	if d["version"] != version {
		t.Errorf("version = %v, 期望 %v", d["version"], version)
	}
	if d["protocol"] != float64(protocolVersion) {
		t.Errorf("protocol = %v", d["protocol"])
	}
}

func TestHelpVariants(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"-h"}, {"--help"}} {
		buf := setupTest(t)
		if code := run(args); code != exitOK {
			t.Fatalf("%v: 退出码 %d, 期望 0", args, code)
		}
		d := data(t, decode(t, buf))
		if s, _ := d["usage"].(string); s == "" {
			t.Errorf("%v: usage 为空", args)
		}
		if cmds, ok := d["commands"].([]any); !ok || len(cmds) != 4 {
			t.Errorf("%v: commands 异常: %v", args, d["commands"])
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"foo"}); code != exitError {
		t.Fatalf("退出码 %d, 期望 1", code)
	}
	if c := errCode(t, decode(t, buf)); c != "bad_args" {
		t.Errorf("code = %q, 期望 bad_args", c)
	}
}

func TestTryMissingKey(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"try"}); code != exitError {
		t.Fatalf("退出码 %d, 期望 1", code)
	}
	if c := errCode(t, decode(t, buf)); c != "bad_args" {
		t.Errorf("code = %q, 期望 bad_args", c)
	}
}

func TestTryFree(t *testing.T) {
	buf := setupTest(t)
	key := randKey(t)
	if code := run([]string{"try", "--key", key}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	if d["free"] != true {
		t.Errorf("free = %v, 期望 true", d["free"])
	}
	if d["key"] != key {
		t.Errorf("key = %v", d["key"])
	}
}

// 同进程用不同句柄持锁：Windows LockFileEx（按句柄归属）与 flock（按 ofd 归属）均会冲突，
// 可模拟"已被其他实例持有"
func TestTryHeld(t *testing.T) {
	key := randKey(t)
	lk, err := lock.Try(key)
	if err != nil {
		t.Fatalf("预持锁失败: %v", err)
	}
	defer lk.Close()
	if err := lk.WriteInfo(key); err != nil {
		t.Fatalf("写入持有者信息失败: %v", err)
	}

	buf := setupTest(t)
	if code := run([]string{"try", "--key", key}); code != exitHeld {
		t.Fatalf("退出码 %d, 期望 3", code)
	}
	d := data(t, decode(t, buf))
	if d["free"] != false {
		t.Errorf("free = %v, 期望 false", d["free"])
	}
	holder, _ := d["holder"].(map[string]any)
	if holder["pid"] != float64(os.Getpid()) {
		t.Errorf("holder.pid = %v, 期望 %d", holder["pid"], os.Getpid())
	}
}

func TestTryWaitTimeout(t *testing.T) {
	key := randKey(t)
	lk, err := lock.Try(key)
	if err != nil {
		t.Fatalf("预持锁失败: %v", err)
	}
	defer lk.Close()

	buf := setupTest(t)
	if code := run([]string{"try", "--key", key, "--wait", "100ms"}); code != exitHeld {
		t.Fatalf("退出码 %d, 期望 3", code)
	}
	d := data(t, decode(t, buf))
	if d["free"] != false {
		t.Errorf("free = %v, 期望 false", d["free"])
	}
}

func TestHoldHeld(t *testing.T) {
	key := randKey(t)
	lk, err := lock.Try(key)
	if err != nil {
		t.Fatalf("预持锁失败: %v", err)
	}
	defer lk.Close()

	buf := setupTest(t)
	if code := run([]string{"hold", "--key", key}); code != exitHeld {
		t.Fatalf("退出码 %d, 期望 3", code)
	}
	d := data(t, decode(t, buf))
	if d["free"] != false {
		t.Errorf("free = %v, 期望 false", d["free"])
	}
}

func TestHoldSuccessAndRelease(t *testing.T) {
	key := randKey(t)
	orig := holdBlock
	holdBlock = func(int) int { return exitOK } // 短路常驻阻塞
	t.Cleanup(func() { holdBlock = orig })

	buf := setupTest(t)
	if code := run([]string{"hold", "--key", key}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	if d["held"] != true {
		t.Errorf("held = %v, 期望 true", d["held"])
	}
	if d["pid"] != float64(os.Getpid()) {
		t.Errorf("pid = %v, 期望 %d", d["pid"], os.Getpid())
	}

	// hold 返回后 defer 已释放锁，此时 try 应为空闲
	buf2 := setupTest(t)
	if code := run([]string{"try", "--key", key}); code != exitOK {
		t.Fatalf("释放后 try 退出码 %d, 期望 0", code)
	}
	if d := data(t, decode(t, buf2)); d["free"] != true {
		t.Errorf("释放后 free = %v, 期望 true", d["free"])
	}
}

func TestList(t *testing.T) {
	key := randKey(t)
	lk, err := lock.Try(key)
	if err != nil {
		t.Fatalf("预持锁失败: %v", err)
	}
	defer lk.Close()
	if err := lk.WriteInfo(key); err != nil {
		t.Fatalf("写入持有者信息失败: %v", err)
	}

	buf := setupTest(t)
	if code := run([]string{"list"}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	entries, ok := d["entries"].([]any)
	if !ok {
		t.Fatalf("entries 非数组: %v", d["entries"])
	}
	found := false
	for _, it := range entries {
		e, _ := it.(map[string]any)
		if e["key"] == key {
			found = true
			if e["held"] != true {
				t.Errorf("持锁中条目 held = %v, 期望 true", e["held"])
			}
		}
	}
	if !found {
		t.Errorf("list 未包含测试 key %q: %v", key, entries)
	}
}

func TestListEmptyIsArray(t *testing.T) {
	// 无锁文件时 entries 必须是 [] 而非 null；目录可能存在其他锁文件，只断言数组性
	buf := setupTest(t)
	if code := run([]string{"list"}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	if _, ok := d["entries"].([]any); !ok {
		t.Fatalf("entries 非数组: %v", d["entries"])
	}
}

func TestPretty(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"--pretty", "version"}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	out := buf.String()
	if !strings.Contains(out, "\n  \"") {
		t.Errorf("--pretty 未缩进输出: %s", out)
	}
	if code := run([]string{"version", "--pretty"}); code != exitOK {
		t.Fatalf("后置 --pretty 退出码 %d, 期望 0", code)
	}
}

func TestBadFlagIsJSONError(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"try", "--key", "k", "--bogus"}); code != exitError {
		t.Fatalf("退出码 %d, 期望 1", code)
	}
	if c := errCode(t, decode(t, buf)); c != "bad_args" {
		t.Errorf("code = %q, 期望 bad_args", c)
	}
}

func TestSubCommandHelp(t *testing.T) {
	buf := setupTest(t)
	if code := run([]string{"try", "-h"}); code != exitOK {
		t.Fatalf("退出码 %d, 期望 0", code)
	}
	d := data(t, decode(t, buf))
	if s, _ := d["usage"].(string); s == "" {
		t.Error("try -h 未输出帮助 usage")
	}
}
