package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDefaultDirUsesAppData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AppData", tmp)
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	want := filepath.Join(tmp, "language_projects", "glmquotawatch")
	if dir != want {
		t.Fatalf("数据目录不符: got %s want %s", dir, want)
	}
}

func TestDefaultDirFallsBackHome(t *testing.T) {
	t.Setenv("AppData", "")
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".language_projects", "glmquotawatch")
	if dir != want {
		t.Fatalf("回退目录不符: got %s want %s", dir, want)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	def := DefaultConfig()
	if cfg.Interval != def.Interval || cfg.Hysteresis != def.Hysteresis || len(cfg.Thresholds) != 4 {
		t.Fatalf("缺失时应返回默认值: %+v", cfg)
	}

	cfg.Token = "token-abcdefgh-1234"
	cfg.Interval = "90s"
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("回读: %v", err)
	}
	if got.Token != cfg.Token || got.Interval != "90s" {
		t.Fatalf("回读不符: %+v", got)
	}

	// 无 .tmp 残留
	entries, _ := os.ReadDir(st.Dir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

func TestConfigCorrupt(t *testing.T) {
	st, _ := Open(t.TempDir())
	if err := os.WriteFile(filepath.Join(st.Dir(), "config.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadConfig(); err == nil {
		t.Fatalf("损坏的 config.json 应报错")
	}
}

func TestStateRoundTripAndCorrupt(t *testing.T) {
	st, _ := Open(t.TempDir())

	s0, err := st.LoadState()
	if err != nil || s0.Version != 0 || s0.Notified != nil {
		t.Fatalf("缺失状态应为零值: %+v err=%v", s0, err)
	}

	want := State{Version: 1, Thresholds: []int{50}, Notified: map[string][]int{"TOKENS_LIMIT:3:5": {50, 60}}}
	if err := st.SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := st.LoadState()
	if err != nil || got.Version != 1 || len(got.Notified["TOKENS_LIMIT:3:5"]) != 2 {
		t.Fatalf("状态回读不符: %+v err=%v", got, err)
	}

	if err := os.WriteFile(filepath.Join(st.Dir(), "state.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = st.LoadState()
	if err == nil {
		t.Fatalf("损坏的 state.json 应报错")
	}
}

func TestAppendSampleMonthlyFile(t *testing.T) {
	st, _ := Open(t.TempDir())
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.Local)

	p1, err := st.AppendSample(now, json.RawMessage(`{"level":"max"}`))
	if err != nil {
		t.Fatalf("AppendSample: %v", err)
	}
	if base := filepath.Base(p1); base != "samples-2026-09.jsonl" {
		t.Fatalf("文件名不符: %s", base)
	}
	if _, err := st.AppendSample(now.Add(time.Minute), json.RawMessage(`{"level":"max"}`)); err != nil {
		t.Fatalf("第二次追加: %v", err)
	}

	data, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("应两行，实际 %d", len(lines))
	}
	var line struct {
		Ts   string          `json:"ts"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatalf("行非合法 JSON: %v", err)
	}
	if string(line.Data) != `{"level":"max"}` {
		t.Fatalf("data 应为原文: %s", line.Data)
	}
}

func TestLock(t *testing.T) {
	st, _ := Open(t.TempDir())

	release, err := st.Lock()
	if err != nil {
		t.Fatalf("首次 Lock: %v", err)
	}

	// 模拟存活占用者（当前进程必然存活）→ 再次加锁被拒
	if err := os.WriteFile(filepath.Join(st.Dir(), "daemon.lock"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = st.Lock()
	var le *LockError
	if !errors.As(err, &le) || le.PID != os.Getpid() {
		t.Fatalf("存活 PID 应拒绝加锁: %v", err)
	}

	// 死 PID（取一个几乎不可能存在的号）→ 可接管
	if err := os.WriteFile(filepath.Join(st.Dir(), "daemon.lock"), []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	release2, err := st.Lock()
	if err != nil {
		t.Fatalf("死 PID 应可接管: %v", err)
	}
	release2()
	release() // 幂等删除
}
