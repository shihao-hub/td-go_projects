package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppendAndReadTail(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenDefault(dir)
	if err != nil {
		t.Fatalf("OpenDefault() 错误: %v", err)
	}
	defer logger.Close()

	ok := true
	events := []Event{
		{Type: TypeMail, UID: 1, From: "a@github.com", Subject: "issue", Action: ActionWhitelisted, Rule: "github"},
		{Type: TypeMail, UID: 2, From: "b@example.com", Subject: "hello", Action: ActionDropped, Reason: "未命中白名单"},
		{Type: TypePush, Thread: "owner/repo#1", Action: ActionPushed, OK: &ok, DurationMS: 12},
	}
	for _, event := range events {
		if err := logger.Append(event); err != nil {
			t.Fatalf("Append() 错误: %v", err)
		}
	}

	got, err := ReadTail(dir, 10)
	if err != nil {
		t.Fatalf("ReadTail() 错误: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadTail() 条数 = %d, want 3", len(got))
	}
	if got[0].Time.IsZero() {
		t.Error("Append 应自动填充时间")
	}
	if got[1].Action != ActionDropped || got[1].Reason == "" {
		t.Errorf("丢弃事件不完整: %+v", got[1])
	}
	if got[2].OK == nil || !*got[2].OK {
		t.Errorf("推送事件 OK 字段错误: %+v", got[2])
	}
}

func TestReadTailLimit(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenDefault(dir)
	if err != nil {
		t.Fatalf("OpenDefault() 错误: %v", err)
	}
	defer logger.Close()

	for i := 1; i <= 5; i++ {
		if err := logger.Append(Event{Type: TypeMail, UID: uint32(i), Action: ActionDropped}); err != nil {
			t.Fatalf("Append() 错误: %v", err)
		}
	}
	got, err := ReadTail(dir, 2)
	if err != nil {
		t.Fatalf("ReadTail() 错误: %v", err)
	}
	if len(got) != 2 || got[0].UID != 4 || got[1].UID != 5 {
		t.Fatalf("应返回最后 2 条，got %+v", got)
	}
}

func TestReadTailSkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	content := "{\"uid\":1,\"action\":\"dropped\"}\nnot-json\n{\"uid\":2,\"action\":\"dropped\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	got, err := ReadTail(dir, 10)
	if err != nil {
		t.Fatalf("ReadTail() 错误: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("坏行应被跳过，got %+v", got)
	}
}

func TestReadTailMissingFile(t *testing.T) {
	got, err := ReadTail(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("文件不存在应返回空，got %+v", got)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	logger, err := Open(dir, 240, 3)
	if err != nil {
		t.Fatalf("Open() 错误: %v", err)
	}
	defer logger.Close()

	for i := 0; i < 20; i++ {
		event := Event{Type: TypeMail, UID: uint32(i), From: "user@example.com", Subject: "a subject long enough", Action: ActionDropped}
		if err := logger.Append(event); err != nil {
			t.Fatalf("Append() 错误: %v", err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("当前审计文件应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".1")); err != nil {
		t.Fatalf("轮转文件应存在: %v", err)
	}
	current, err := ReadTail(dir, 100)
	if err != nil {
		t.Fatalf("ReadTail() 错误: %v", err)
	}
	if len(current) == 0 {
		t.Fatal("轮转后当前文件仍应有事件")
	}
	if len(current) >= 20 {
		t.Fatal("轮转后当前文件不应包含全部事件")
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenDefault(dir)
	if err != nil {
		t.Fatalf("OpenDefault() 错误: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() 错误: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("重复 Close 应安全: %v", err)
	}
	if err := logger.Append(Event{Type: TypeMail}); err == nil {
		t.Fatal("Close 后 Append 应报错")
	}
}

func TestConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenDefault(dir)
	if err != nil {
		t.Fatalf("OpenDefault() 错误: %v", err)
	}
	defer logger.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				event := Event{
					Time:   time.Now(),
					Type:   TypeMail,
					UID:    uint32(id*20 + j),
					Action: ActionDropped,
				}
				if err := logger.Append(event); err != nil {
					t.Errorf("并发 Append() 错误: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	got, err := ReadTail(dir, 1000)
	if err != nil {
		t.Fatalf("ReadTail() 错误: %v", err)
	}
	if len(got) != 160 {
		t.Fatalf("并发写入条数 = %d, want 160", len(got))
	}
}

func TestEventJSONShape(t *testing.T) {
	event := Event{Time: time.Now().UTC(), Type: TypeMail, UID: 7, Action: ActionDropped, Reason: "未命中白名单"}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("序列化错误: %v", err)
	}
	for _, part := range []string{`"type":"mail"`, `"uid":7`, `"action":"dropped"`} {
		if !strings.Contains(string(data), part) {
			t.Fatalf("JSON 缺少 %s: %s", part, data)
		}
	}
	if strings.Contains(string(data), `"ok"`) {
		t.Fatalf("mail 事件不应出现 ok 字段: %s", data)
	}
}
