package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"glmquotawatch/internal/store"
)

const validToken = "8db97e3da78c44abb0a72ce3ae423209.xK2juMP0d8xb236g"

// newTestSvc 构造 httptest 上游 + 隔离数据目录的 Service。
// percentage 由指针共享，测试中改 *pct 即可切换上游响应；
// 不采样的测试可传 nil。
func newTestSvc(t *testing.T, pct *int64) *Service {
	t.Helper()
	reset := time.Now().Add(4*time.Hour + 43*time.Minute).UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p := int64(2)
		if pct != nil {
			p = *pct
		}
		fmt.Fprintf(w, `{"code":200,"msg":"操作成功","success":true,"data":{"level":"max","limits":[`+
			`{"type":"TIME_LIMIT","unit":5,"number":1,"percentage":5},`+
			`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":%d}]}}`, p, reset)
	}))
	t.Cleanup(srv.Close)

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return New(st, WithBaseURL(srv.URL))
}

func svcErrCode(t *testing.T, err error) string {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("期望 *service.Error，实际 %T: %v", err, err)
	}
	return se.Code
}

func TestSetTokenValidation(t *testing.T) {
	svc := newTestSvc(t, nil)

	if _, err := svc.SetToken("short"); svcErrCode(t, err) != "bad_args" {
		t.Fatalf("短 token 应 bad_args: %v", err)
	}
	view, err := svc.SetToken("  " + validToken + "  ") // trim 生效
	if err != nil {
		t.Fatalf("SetToken: %v", err)
	}
	if !view.HasToken || view.Token != MaskToken(validToken) {
		t.Fatalf("视图应脱敏: %+v", view)
	}
	if strings.Contains(view.Token, validToken) {
		t.Fatalf("视图不应含明文 token")
	}
}

func TestMaskToken(t *testing.T) {
	if got := MaskToken(validToken); got != "8db97e3d…236g" {
		t.Fatalf("长 token 脱敏不符: %s", got)
	}
	if got := MaskToken("short"); got != "*****" {
		t.Fatalf("短 token 应全星: %s", got)
	}
	if MaskToken("") != "" {
		t.Fatalf("空 token 应为空")
	}
}

func TestRemoveTokenIdempotent(t *testing.T) {
	svc := newTestSvc(t, nil)
	if _, err := svc.RemoveToken(); err != nil {
		t.Fatalf("未配置时 remove 应幂等成功: %v", err)
	}
	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}
	view, err := svc.RemoveToken()
	if err != nil || view.HasToken {
		t.Fatalf("remove 后应无 token: %+v err=%v", view, err)
	}
}

func TestSetConfigKeys(t *testing.T) {
	svc := newTestSvc(t, nil)

	// 合法路径
	if _, err := svc.SetConfig("interval", "90s"); err != nil {
		t.Fatalf("interval: %v", err)
	}
	view, _ := svc.GetConfig()
	if view.Interval != "1m30s" {
		t.Fatalf("interval 应规范化: %s", view.Interval)
	}
	if view, err := svc.SetConfig("thresholds", "80,50,60"); err != nil || !reflect.DeepEqual(view.Thresholds, []int{50, 60, 80}) {
		t.Fatalf("thresholds 应排序去重: %+v err=%v", view, err)
	}
	if _, err := svc.SetConfig("thresholds", "50,50"); err != nil {
		t.Fatalf("重复项应去重不报错: %v", err)
	}
	if _, err := svc.SetConfig("hysteresis", "10"); err != nil {
		t.Fatalf("hysteresis: %v", err)
	}
	if view, err := svc.SetConfig("silent", "true"); err != nil || !view.Silent {
		t.Fatalf("silent: %+v err=%v", view, err)
	}

	// 非法路径（错误码断言）
	for _, c := range []struct{ key, val, code string }{
		{"interval", "bogus", "bad_value"},
		{"interval", "-5m", "bad_value"},
		{"thresholds", "0", "bad_value"},
		{"thresholds", "abc", "bad_value"},
		{"thresholds", "", "bad_value"},
		{"hysteresis", "40", "bad_value"},
		{"silent", "x", "bad_value"},
		{"foo", "1", "unknown_key"},
	} {
		if _, err := svc.SetConfig(c.key, c.val); svcErrCode(t, err) != c.code {
			t.Fatalf("%s=%q 应 %s: %v", c.key, c.val, c.code, err)
		}
	}
}

func TestSampleOnceNoToken(t *testing.T) {
	svc := newTestSvc(t, nil)
	if _, _, err := svc.SampleOnce(context.Background()); svcErrCode(t, err) != "no_token" {
		t.Fatalf("未配 token 应 no_token: %v", err)
	}
}

func TestSampleOnceFlowAndPersistence(t *testing.T) {
	pct := int64(2)
	svc := newTestSvc(t, &pct)
	ctx := context.Background()

	if _, err := svc.SetToken(validToken); err != nil {
		t.Fatal(err)
	}
	view, outs, err := svc.SampleOnce(ctx)
	if err != nil {
		t.Fatalf("SampleOnce: %v", err)
	}
	if view.Level != "max" || len(view.Windows) != 1 {
		t.Fatalf("视图不符: %+v", view)
	}
	w := view.Windows[0]
	if w.Percentage != 2 || w.ResetIn == "" || w.NextResetTime == "" {
		t.Fatalf("窗口视图不符: %+v", w)
	}
	if len(outs) != 1 || outs[0].Notify {
		t.Fatalf("低用量不应通知: %+v", outs)
	}

	// 采样历史已落盘（samples-*.jsonl 一行）
	entries, _ := os.ReadDir(svc.st.Dir())
	var samples int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "samples-") {
			samples++
		}
	}
	if samples != 1 {
		t.Fatalf("应有 1 个采样历史文件，实际 %d", samples)
	}
	raw, _ := os.ReadFile(filepath.Join(svc.st.Dir(), "samples-"+time.Now().Format("2006-01")+".jsonl"))
	if !strings.Contains(string(raw), `"TOKENS_LIMIT"`) {
		t.Fatalf("历史应含 data 原文: %s", raw)
	}

	// 用量冲到 92 → 新档位触发（通知由 daemon 负责，此处只看评估结果）
	pct = 92
	_, outs, err = svc.SampleOnce(ctx)
	if err != nil {
		t.Fatalf("第二次 SampleOnce: %v", err)
	}
	if len(outs) != 1 || !outs[0].Notify || outs[0].Highest != 90 {
		t.Fatalf("92%% 应触发 90 档: %+v", outs)
	}

	// 再采一次同值 → 不重复触发（state 已持久化记账）
	_, outs, err = svc.SampleOnce(ctx)
	if err != nil || outs[0].Notify {
		t.Fatalf("同档位不应重复触发: %+v err=%v", outs, err)
	}
}

func TestSampleOnceUpstreamCodes(t *testing.T) {
	// 非 2xx → api_error
	st, _ := store.Open(t.TempDir())
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()
	svc := New(st, WithBaseURL(bad.URL))
	_, _ = svc.SetToken(validToken)
	if _, _, err := svc.SampleOnce(context.Background()); svcErrCode(t, err) != "api_error" {
		t.Fatalf("非 2xx 应 api_error: %v", err)
	}

	// 200 但业务码非 200 → upstream_error
	biz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"code":1006,"msg":"无权访问","success":false,"data":null}`)
	}))
	defer biz.Close()
	st2, _ := store.Open(t.TempDir())
	svc2 := New(st2, WithBaseURL(biz.URL))
	_, _ = svc2.SetToken(validToken)
	if _, _, err := svc2.SampleOnce(context.Background()); svcErrCode(t, err) != "upstream_error" {
		t.Fatalf("业务码非 200 应 upstream_error: %v", err)
	}
}

// TestViewThresholdsNeverNull 手改 config.json 把 thresholds 写成 null 时，
// 视图仍须序列化为 []：outputSchema（已不含 null 分支）会被 go-sdk 用来
// 校验 structuredContent，null 会导致校验失败。
func TestViewThresholdsNeverNull(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"thresholds": null}`), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	view, err := New(st).GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if view.Thresholds == nil {
		t.Fatalf("视图 Thresholds 不应为 nil")
	}
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"thresholds":[]`) || strings.Contains(string(b), `"thresholds":null`) {
		t.Fatalf("thresholds 应序列化为 [] 而非 null: %s", b)
	}
}
