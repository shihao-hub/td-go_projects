package model

import "strings"

// 系统标签：固定枚举，每档一种颜色（BGR，ListView custom draw 直接用）。
// 约束：一条 Entry 至多一个系统标签 + 一个用户标签；
// 系统标签是"状态"语义（todo 感），用户标签只是自由文本描述。
type sysTagDef struct {
	Key   string
	Label string
	Color uint32
}

const SysTagNone = "" // 无系统标签

// rgb 生成 BGR 值（ListView custom draw 用），与 win32.RGB 等价的本地实现。
func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

// SysTagDefs 集合顺序即下拉框展示顺序。
var SysTagDefs = []sysTagDef{
	{"todo", "待完善", rgb(0xE0, 0x8A, 0x00)},   // 橙
	{"verify", "待验证", rgb(0x20, 0x70, 0xC0)}, // 蓝
	{"broken", "有问题", rgb(0xC0, 0x30, 0x30)}, // 红
	{"stable", "稳定", rgb(0x30, 0x90, 0x40)},  // 绿
}

// sysTagKeyOf 集合内 key → 显示名；未命中返回空串。
func SysTagLabel(key string) string {
	for _, t := range SysTagDefs {
		if t.Key == key {
			return t.Label
		}
	}
	return ""
}

// SysTagColor 集合内 key → 颜色；未命中 ok=false（调用方走默认色）。
func SysTagColor(key string) (uint32, bool) {
	for _, t := range SysTagDefs {
		if t.Key == key {
			return t.Color, true
		}
	}
	return 0, false
}

// SanitizeSysTag 载入/落盘前清洗：手改 config.json 塞进未知 key 时归零。
func SanitizeSysTag(key string) string {
	if SysTagLabel(key) == "" {
		return SysTagNone
	}
	return key
}

// TagColumnText 标签列文本：● 系统标签 + 用户标签（描述），两者可有可无。
func (e *Entry) TagColumnText() string {
	var b strings.Builder
	if lbl := SysTagLabel(e.SysTag); lbl != "" {
		b.WriteString("● ")
		b.WriteString(lbl)
	}
	if e.UserTag != "" {
		if b.Len() > 0 {
			b.WriteString("　")
		}
		b.WriteString(e.UserTag)
	}
	return b.String()
}

// CountBySysTag 统计某系统标签的条数（状态栏/todo 概览用）。
func (s *Store) CountBySysTag(key string) int {
	n := 0
	for _, e := range s.Entries {
		if e.SysTag == key {
			n++
		}
	}
	return n
}
