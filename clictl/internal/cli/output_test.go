package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

// saveGlobals 保存全局 Pretty/Ascii 并注册恢复，避免用例间污染
func saveGlobals(t *testing.T) {
	t.Helper()
	pretty, ascii := Pretty, Ascii
	t.Cleanup(func() { Pretty, Ascii = pretty, ascii })
}

// uesc 程序化构造字面 "\uXXXX"（含反斜杠）期望串：源码里手写该形态
// 容易被工具链转义歧义吞掉，统一走拼接
func uesc(hex string) string {
	return string(rune(0x5C)) + hex
}

func TestEscapeNonASCII(t *testing.T) {
	// oracle：strconv.QuoteToASCII 是标准库对"非 ASCII → \u 小写 hex"的
	// 独立实现，去引号后即期望串（BMP 内与 escapeNonASCII 同形）
	qascii := func(s string) string {
		q := strconv.QuoteToASCII(s)
		return q[1 : len(q)-1]
	}
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"纯 ASCII 原样返回", `{"ok":true,"data":[1,2,3]}`, `{"ok":true,"data":[1,2,3]}`},
		{"中文双字转义", "中文", qascii("中文")},
		{"中英混合只转非 ASCII 部分", "a中b", qascii("a中b")},
		{"边界 U+007F 属 ASCII 不转义", "\x7f", "\x7f"},
		{"边界 U+0080 首个非 ASCII", "\xc2\x80", qascii("\xc2\x80")},
		{"边界 U+FFFF", "￿", qascii("￿")},
		{"既有 ASCII 转义序列不二次处理", `{"m":"` + uesc(`u4e2d`) + `"}`, `{"m":"` + uesc(`u4e2d`) + `"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(escapeNonASCII([]byte(c.input))); got != c.want {
				t.Errorf("escapeNonASCII(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestEscapeNonASCIISurrogatePair(t *testing.T) {
	// BMP 外字符必须拆代理对（JSON 标准）：hi=0xD800+(v>>10)，lo=0xDC00+(v&0x3FF)
	cases := []struct {
		name string
		v    rune // 超出 BMP 的完整码点
		hi   rune
		lo   rune
	}{
		{"emoji 代理对", '😀', 0xD83D, 0xDE00},
		{"边界 U+10000 最小代理对", '𐀀', 0xD800, 0xDC00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := uesc(fmt.Sprintf("u%04x", c.hi)) + uesc(fmt.Sprintf("u%04x", c.lo))
			if got := string(escapeNonASCII([]byte(string(c.v)))); got != want {
				t.Errorf("escapeNonASCII(%q) = %q, want %q", string(c.v), got, want)
			}
		})
	}
}

func TestEscapeNonASCIIInvalidUTF8(t *testing.T) {
	// 无效字节防御分支：正常路径 Encoder 已净化不可达，直测字节级兜底
	if got := string(escapeNonASCII([]byte{0xff})); got != uesc("ufffd") {
		t.Errorf("escapeNonASCII(0xff) = %q, want %q", got, uesc("ufffd"))
	}
}

func TestMarshalAscii(t *testing.T) {
	payload := map[string]any{"ok": true, "data": map[string]string{"msg": "中文消息"}}

	t.Run("默认双 false 中文原样", func(t *testing.T) {
		saveGlobals(t)
		Pretty, Ascii = false, false
		b, err := marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte("中文消息")) {
			t.Errorf("默认模式应保留中文原文, got %s", b)
		}
	})

	t.Run("Ascii 产物全 ASCII 且 Unmarshal 还原相等", func(t *testing.T) {
		saveGlobals(t)
		Pretty, Ascii = false, true
		b, err := marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range b {
			if c >= 0x80 {
				t.Fatalf("Ascii 模式产物应全为 ASCII, 含字节 0x%02x: %s", c, b)
			}
		}
		// "中" = U+4E2D，产物应含其转义形态
		if !bytes.Contains(b, []byte(uesc("u4e2d"))) {
			t.Errorf("应含中文字符的转义形态, got %s", b)
		}
		var back struct {
			OK   bool              `json:"ok"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("转义产物必须是合法 JSON: %v", err)
		}
		if !back.OK || back.Data["msg"] != "中文消息" {
			t.Errorf("JSON.parse 还原不相等: ok=%v msg=%q", back.OK, back.Data["msg"])
		}
	})

	t.Run("Pretty+Ascii 组合", func(t *testing.T) {
		saveGlobals(t)
		Pretty, Ascii = true, true
		b, err := marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte("\n  ")) {
			t.Errorf("Pretty+Ascii 应有缩进换行, got %s", b)
		}
		for _, c := range b {
			if c >= 0x80 {
				t.Fatalf("Pretty+Ascii 产物应全为 ASCII, 含字节 0x%02x: %s", c, b)
			}
		}
		var back struct {
			OK   bool              `json:"ok"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("组合模式产物必须是合法 JSON: %v", err)
		}
	})
}

func TestStripGlobalFlags(t *testing.T) {
	saveGlobals(t)
	Pretty, Ascii = false, false
	got := stripGlobalFlags([]string{"--pretty", "list", "--ascii", "--status", "active"})
	want := []string{"list", "--status", "active"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stripGlobalFlags = %v, want %v", got, want)
	}
	if !Pretty || !Ascii {
		t.Errorf("剥离后应置位 Pretty/Ascii, got Pretty=%v Ascii=%v", Pretty, Ascii)
	}
}
