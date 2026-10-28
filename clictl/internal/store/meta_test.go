package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// groupOf 取校验结果里的 group 数组（断言辅助）。
func groupOf(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	if raw == nil {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("解析规范化 meta 失败: %v", err)
	}
	val, ok := obj["group"]
	if !ok {
		return nil
	}
	var groups []string
	if err := json.Unmarshal(val, &groups); err != nil {
		t.Fatalf("group 应序列化为数组，实际 %s: %v", val, err)
	}
	return groups
}

func TestValidateMetaGroupNormalization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{name: "旧单字符串归一为数组", in: `{"group":"Dev"}`, want: []string{"Dev"}},
		{name: "数组直传", in: `{"group":["dev","ops"]}`, want: []string{"dev", "ops"}},
		{name: "首尾空白裁剪", in: `{"group":["  dev  "," ops"]}`, want: []string{"dev", "ops"}},
		{name: "空项剔除", in: `{"group":["dev","","   ","ops"]}`, want: []string{"dev", "ops"}},
		{name: "精确去重且大小写保留", in: `{"group":["Dev","dev","Dev"]}`, want: []string{"Dev", "dev"}},
		{name: "输出有序", in: `{"group":["zeta","alpha","mid"]}`, want: []string{"alpha", "mid", "zeta"}},
		{name: "空字符串剔 key", in: `{"group":"   "}`, want: nil},
		{name: "空数组剔 key", in: `{"group":[]}`, want: nil},
		{name: "空项数组剔 key", in: `{"group":[""," "]}`, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			norm, err := ValidateMeta([]byte(tc.in))
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if tc.want == nil {
				if norm != nil {
					t.Fatalf("应剔除整个 group key，实际 %s", norm)
				}
				return
			}
			got := groupOf(t, norm)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("group 归一不符：want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestValidateMetaGroupLimit(t *testing.T) {
	// 恰好 8 项通过
	ok := `{"group":["g1","g2","g3","g4","g5","g6","g7","g8"]}`
	if _, err := ValidateMeta([]byte(ok)); err != nil {
		t.Fatalf("8 个分组应通过: %v", err)
	}
	// 9 项超限
	tooMany := `{"group":["g1","g2","g3","g4","g5","g6","g7","g8","g9"]}`
	if _, err := ValidateMeta([]byte(tooMany)); !isMetaCode(err, "meta_invalid") {
		t.Fatalf("9 个分组应报 meta_invalid，实际 %v", err)
	}
	// 去重后不超限
	deduped := `{"group":["a","a","a","a","a","a","a","a","b"]}`
	if _, err := ValidateMeta([]byte(deduped)); err != nil {
		t.Fatalf("去重后仅 2 项应通过: %v", err)
	}
	// 单项 32 字节通过，33 字节失败
	okLen := `{"group":["` + strings.Repeat("a", 32) + `"]}`
	if _, err := ValidateMeta([]byte(okLen)); err != nil {
		t.Fatalf("32 字节分组名应通过: %v", err)
	}
	badLen := `{"group":["` + strings.Repeat("a", 33) + `"]}`
	if _, err := ValidateMeta([]byte(badLen)); !isMetaCode(err, "meta_invalid") {
		t.Fatalf("超长分组名应报 meta_invalid，实际 %v", err)
	}
}

func TestValidateMetaGroupTypeError(t *testing.T) {
	for _, in := range []string{`{"group":123}`, `{"group":{"a":1}}`, `{"group":true}`, `{"group":["a",1]}`} {
		if _, err := ValidateMeta([]byte(in)); !isMetaCode(err, "meta_invalid") {
			t.Fatalf("%s 应报 meta_invalid，实际 %v", in, err)
		}
	}
}

// group 与 tags 共存时仍在总字节上限内。
func TestValidateMetaGroupWithTags(t *testing.T) {
	norm, err := ValidateMeta([]byte(`{"source":"npm","tags":["dev"],"group":["dev","ops"]}`))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(norm) > metaMaxBytes {
		t.Fatalf("序列化结果超过上限: %d", len(norm))
	}
	if got := groupOf(t, norm); len(got) != 2 || got[0] != "dev" || got[1] != "ops" {
		t.Fatalf("group 往返不符: %v", got)
	}
	if !strings.Contains(string(norm), `"tags":["dev"]`) {
		t.Fatalf("tags 应保留: %s", norm)
	}
}

func isMetaCode(err error, code string) bool {
	var me *MetaError
	if !errors.As(err, &me) {
		return false
	}
	return me.Code == code
}
