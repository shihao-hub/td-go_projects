package quota

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"glmquotawatch-gui/internal/api"
	"glmquotawatch-gui/internal/store"
)

var (
	now        = time.Date(2026, 9, 15, 10, 0, 0, 0, time.Local)
	testKey    = "TOKENS_LIMIT:3:5"
	defThreshs = []int{50, 60, 80, 90}
)

func testCfg() store.Config {
	return store.Config{Thresholds: defThreshs, Hysteresis: 5}
}

func tokenLimit(pct int64) api.Limit {
	reset := now.Add(4*time.Hour + 43*time.Minute).UnixMilli()
	return api.Limit{Type: "TOKENS_LIMIT", Unit: 3, Number: 5, Percentage: &pct, NextResetTime: &reset}
}

func evalCfg(cfg store.Config, prev store.State, limits ...api.Limit) (store.State, []Outcome) {
	return Evaluate(cfg, prev, limits, now)
}

func notifiedOf(st store.State, key string) []int {
	return st.Notified[key]
}

func TestFirstSampleBelowNoNotify(t *testing.T) {
	next, outs := evalCfg(testCfg(), store.State{}, tokenLimit(2))
	if len(outs) != 1 || outs[0].Notify {
		t.Fatalf("低用量不应通知: %+v", outs)
	}
	if len(notifiedOf(next, testKey)) != 0 {
		t.Fatalf("不应记账: %+v", next)
	}
}

func TestFirstSampleCrossJumpRecordsAll(t *testing.T) {
	// 48% → 92% 一轮跳三档：只通知一次、报最高档，但记账全量
	next, outs := evalCfg(testCfg(), store.State{}, tokenLimit(92))
	if len(outs) != 1 || !outs[0].Notify {
		t.Fatalf("应通知: %+v", outs)
	}
	if outs[0].Highest != 90 {
		t.Fatalf("应报最高新档 90: %d", outs[0].Highest)
	}
	want := "5h 窗口已消耗 92%（阈值 90%），距下次刷新还有 4h43m"
	if outs[0].Message != want {
		t.Fatalf("文案不符:\n got %s\nwant %s", outs[0].Message, want)
	}
	if got := outs[0].Newly; !reflect.DeepEqual(got, []int{50, 60, 80, 90}) {
		t.Fatalf("应保留全部待确认新档: %v", got)
	}
	if got := notifiedOf(next, testKey); !reflect.DeepEqual(got, []int{50, 60, 80, 90}) {
		t.Fatalf("应记账全量 F: %v", got)
	}
}

func TestEqualThresholdTriggers(t *testing.T) {
	_, outs := evalCfg(testCfg(), store.State{}, tokenLimit(50))
	if !outs[0].Notify || outs[0].Highest != 50 {
		t.Fatalf("恰好达到阈值应触发: %+v", outs[0])
	}
}

func TestNoRepeatSameLevel(t *testing.T) {
	prev := store.State{Version: 1, Thresholds: defThreshs, Notified: map[string][]int{testKey: {50, 60, 80, 90}}}
	_, outs := evalCfg(testCfg(), prev, tokenLimit(92))
	if outs[0].Notify {
		t.Fatalf("已记账档位不应重复通知")
	}
}

func TestHysteresisHolds(t *testing.T) {
	prev := store.State{Version: 1, Thresholds: defThreshs, Notified: map[string][]int{testKey: {50, 60, 80, 90}}}
	// 92 → 55：仍在滞回带内，不清空也不通知
	next, outs := evalCfg(testCfg(), prev, tokenLimit(55))
	if outs[0].Notify || outs[0].Cleared {
		t.Fatalf("滞回带内不应动作: %+v", outs[0])
	}
	if len(notifiedOf(next, testKey)) != 4 {
		t.Fatalf("滞回带内不应清空记账")
	}
	// 55 → 85：仍未清空，不得再通知
	_, outs = evalCfg(testCfg(), next, tokenLimit(85))
	if outs[0].Notify {
		t.Fatalf("未重置告警记录不应通知")
	}
}

func TestHysteresisClearsRearms(t *testing.T) {
	prev := store.State{Version: 1, Thresholds: defThreshs, Notified: map[string][]int{testKey: {50, 60, 80, 90}}}
	// 92 → 44（低于 50-5）：清空
	next, outs := evalCfg(testCfg(), prev, tokenLimit(44))
	if !outs[0].Cleared {
		t.Fatalf("跌破滞回线应清空: %+v", outs[0])
	}
	if len(notifiedOf(next, testKey)) != 0 {
		t.Fatalf("清空后不应有记账")
	}
	// 44 → 51：重置告警记录后再次触发 50
	_, outs = evalCfg(testCfg(), next, tokenLimit(51))
	if !outs[0].Notify || outs[0].Highest != 50 {
		t.Fatalf("重置告警记录后应再通知 50: %+v", outs[0])
	}
}

func TestFingerprintReset(t *testing.T) {
	prev := store.State{Version: 1, Thresholds: []int{50}, Notified: map[string][]int{testKey: {50}}}
	cfg := store.Config{Thresholds: []int{50, 90}, Hysteresis: 5}
	_, outs := evalCfg(cfg, prev, tokenLimit(91))
	if !outs[0].Notify || outs[0].Highest != 90 {
		t.Fatalf("阈值指纹变更应整体重置告警记录: %+v", outs[0])
	}
}

func TestMultipleWindowsIndependent(t *testing.T) {
	pct5h, pctWeek := int64(92), int64(2)
	l5h := tokenLimit(pct5h)
	week := api.Limit{Type: "TOKENS_LIMIT", Unit: 3, Number: 168, Percentage: &pctWeek}
	next, outs := evalCfg(testCfg(), store.State{}, l5h, week)
	if len(outs) != 2 {
		t.Fatalf("应评估两个窗口: %d", len(outs))
	}
	if !outs[0].Notify || outs[0].Key != "TOKENS_LIMIT:3:5" {
		t.Fatalf("5h 窗口应通知: %+v", outs[0])
	}
	if outs[1].Notify {
		t.Fatalf("周窗口低用量不应通知")
	}
	if len(notifiedOf(next, "TOKENS_LIMIT:3:5")) != 4 || len(notifiedOf(next, "TOKENS_LIMIT:3:168")) != 0 {
		t.Fatalf("两窗口记账应独立: %+v", next.Notified)
	}
}

func TestTimeLimitAndNilPercentageIgnored(t *testing.T) {
	pct := int64(92)
	time_ := api.Limit{Type: "TIME_LIMIT", Unit: 5, Number: 1, Percentage: &pct}
	noPct := api.Limit{Type: "TOKENS_LIMIT", Unit: 3, Number: 5}
	next, outs := evalCfg(testCfg(), store.State{}, time_, noPct)
	if len(outs) != 0 {
		t.Fatalf("应全部跳过: %+v", outs)
	}
	if len(next.Notified) != 0 {
		t.Fatalf("不应有记账")
	}
}

func TestNoResetTimeOmitsSuffix(t *testing.T) {
	pct := int64(92)
	l := api.Limit{Type: "TOKENS_LIMIT", Unit: 3, Number: 5, Percentage: &pct} // 无 nextResetTime
	_, outs := evalCfg(testCfg(), store.State{}, l)
	if !outs[0].Notify {
		t.Fatal("应通知")
	}
	want := "5h 窗口已消耗 92%（阈值 90%）"
	if outs[0].Message != want {
		t.Fatalf("缺 nextResetTime 应省略后半句:\n got %s\nwant %s", outs[0].Message, want)
	}
}

func TestExpiredResetTimeOmitsSuffix(t *testing.T) {
	pct := int64(92)
	expired := now.Add(-time.Hour).UnixMilli()
	l := api.Limit{Type: "TOKENS_LIMIT", Unit: 3, Number: 5, Percentage: &pct, NextResetTime: &expired}
	_, outs := evalCfg(testCfg(), store.State{}, l)
	if strings.Contains(outs[0].Message, "距下次刷新") {
		t.Fatalf("已过期的刷新时间不应输出时长: %s", outs[0].Message)
	}
}

func TestFormatResetIn(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-5 * time.Minute, ""},
		{30 * time.Second, "1m"},
		{37 * time.Minute, "37m"},
		{4*time.Hour + 43*time.Minute + 29*time.Second, "4h43m"},
		{24 * time.Hour, "1d"},
		{26 * time.Hour, "1d2h"},
		{25 * 24 * time.Hour, "25d"},
	}
	for _, c := range cases {
		if got := FormatResetIn(c.d); got != c.want {
			t.Fatalf("FormatResetIn(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	cases := []struct {
		l    api.Limit
		want string
	}{
		{api.Limit{Unit: 3, Number: 5}, "5h 窗口"},
		{api.Limit{Unit: 3, Number: 168}, "168h 窗口"},
		{api.Limit{Unit: 5, Number: 1}, "1 个月窗口"},
		{api.Limit{Unit: 2, Number: 7}, "7×unit2 窗口"},
	}
	for _, c := range cases {
		if got := WindowLabel(c.l); got != c.want {
			t.Fatalf("WindowLabel(%+v) = %q, want %q", c.l, got, c.want)
		}
	}
}
