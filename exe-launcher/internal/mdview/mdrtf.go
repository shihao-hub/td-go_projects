package mdview

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// MDToRTF 把 markdown 子集转换为 RTF，供 RichEdit EM_STREAMIN 渲染。
// 支持子集：h1-h6 标题、粗体、斜体、行内代码、围栏代码块、无序/有序列表、
// 引用块、分隔线、链接（蓝色文本 + 灰色 URL）。不支持表格/图片/嵌套结构。
//
// colortbl 索引：1=正文黑 2=链接蓝 3=代码底浅灰 4=辅助灰。

const (
	rtfBlueIdx = 2
	rtfCodeBg  = 3
	rtfGrayIdx = 4
)

const rtfHeader = `{\rtf1\ansi\deff0{\fonttbl{\f0\fnil Microsoft YaHei UI;}{\f1\fmodern Consolas;}}` + "\n" +
	`{\colortbl;\red0\green0\blue0;\red0\green102\blue204;\red235\green235\blue235;\red110\green110\blue110;}` + "\n"

// rtfEscapeWriter 把一个 rune 按 RTF 规则写出：
// \ { } 转义；ASCII 可见字符直写；其余按 UTF-16 码元 \uN? 转义（含代理对）。
func rtfEscapeWriter(b *strings.Builder, r rune) {
	switch r {
	case '\\', '{', '}':
		b.WriteByte('\\')
		b.WriteRune(r)
		return
	}
	if r >= 0x20 && r < 0x7F {
		b.WriteRune(r)
		return
	}
	if r < 0x20 { // 控制字符：制表符按 4 空格，其余丢弃
		if r == '\t' {
			b.WriteString("    ")
		}
		return
	}
	for _, u := range utf16.Encode([]rune{r}) {
		fmt.Fprintf(b, "\\u%d?", int16(u))
	}
}

func rtfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		rtfEscapeWriter(&b, r)
	}
	return b.String()
}

// inlineRTF 解析行内元素：**粗体**、*斜体*、`代码`、[文本](链接)。
// 未闭合的标记按普通文本原样输出。
func inlineRTF(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				b.WriteString(`\b `)
				b.WriteString(inlineRTF(s[i+2 : i+2+end]))
				b.WriteString(`\b0 `)
				i += 2 + end + 2
				continue
			}
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end > 0 {
				b.WriteString(`\f1\fs19\highlight3 `)
				b.WriteString(rtfEscape(s[i+1 : i+1+end]))
				b.WriteString(`\highlight0\f0\fs20 `)
				i += 1 + end + 1
				continue
			}
		case s[i] == '[':
			if cb := strings.IndexByte(s[i:], ']'); cb > 0 && strings.HasPrefix(s[i+cb+1:], "(") {
				if cp := strings.IndexByte(s[i+cb+1:], ')'); cp > 1 {
					label := s[i+1 : i+cb]
					url := s[i+cb+2 : i+cb+1+cp]
					fmt.Fprintf(&b, `\cf%d `, rtfBlueIdx)
					b.WriteString(rtfEscape(label))
					if url != "" && url != label {
						fmt.Fprintf(&b, `\cf%d (%s)\cf0 `, rtfGrayIdx, rtfEscape(url))
					} else {
						b.WriteString(`\cf0 `)
					}
					i = i + cb + 1 + cp + 1
					continue
				}
			}
		case s[i] == '*':
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 {
				b.WriteString(`\i `)
				b.WriteString(inlineRTF(s[i+1 : i+1+end]))
				b.WriteString(`\i0 `)
				i += 1 + end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rtfEscapeWriter(&b, r)
		i += size
	}
	return b.String()
}

func headingRTF(level int, text string) string {
	fs := map[int]int{1: 36, 2: 32, 3: 28}[level]
	if fs == 0 {
		fs = 24 // h4-h6 同级
	}
	return fmt.Sprintf(`\pard\fs%d\b %s\b0\fs20\par`, fs, inlineRTF(text))
}

// MDToRTF 主入口：逐行状态机 + 行内解析。
func MDToRTF(src string) string {
	var out strings.Builder
	out.WriteString(rtfHeader)
	inCode := false
	for _, ln := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(ln, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			out.WriteString(`\pard\f1\fs19\highlight3 `)
			out.WriteString(rtfEscape(ln))
			out.WriteString(`\highlight0\f0\fs20\par`)
			continue
		}
		switch {
		case ln == "":
			// 空行：段落间距由 RichEdit 默认 \sa 提供，无需输出
		case isHr(ln):
			out.WriteString(`\pard\brdrb\brdrs\brdrw10\brsp20 \par`)
		case headingLevel(ln) > 0:
			n := headingLevel(ln)
			out.WriteString(headingRTF(n, strings.TrimSpace(ln[n+1:])))
		case strings.HasPrefix(ln, ">"):
			out.WriteString(fmt.Sprintf(`\pard\li284\cf%d\i %s\i0\cf0\li0\par`,
				rtfGrayIdx, inlineRTF(strings.TrimSpace(strings.TrimPrefix(ln, ">")))))
		case isBullet(ln):
			fmt.Fprintf(&out, `\pard\fi-284\li284 \u8226?  %s\par`,
				inlineRTF(strings.TrimSpace(ln[1:])))
		case orderedLabel(ln) != "":
			label := strings.TrimRight(orderedLabel(ln), " ")
			fmt.Fprintf(&out, `\pard\fi-340\li340 %s %s\par`,
				rtfEscape(label), inlineRTF(strings.TrimSpace(ln[len(label):])))
		default:
			// `WriteString(a + b + c)` 会先把拼接结果整体分配成一个临时字符串，再拷贝进 out，属于多余的一次分配+拷贝。修复方式是拆成三次写
			// out.WriteString(`\pard\fs20 ` + inlineRTF(ln) + `\par`)
			out.WriteString(`\pard\fs20 `)
			out.WriteString(inlineRTF(ln))
			out.WriteString(`\par`)

		}
	}
	out.WriteString("}")
	return out.String()
}

// isHr 判定 --- / *** / ___ 分隔线（3 个以上）。
func isHr(ln string) bool {
	if len(ln) < 3 {
		return false
	}
	c := ln[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(ln); i++ {
		if ln[i] != c {
			return false
		}
	}
	return true
}

func headingLevel(ln string) int {
	n := 0
	for n < len(ln) && ln[n] == '#' {
		n++
	}
	if n >= 1 && n <= 6 && n < len(ln) && ln[n] == ' ' {
		return n
	}
	return 0
}

func isBullet(ln string) bool {
	if len(ln) < 2 {
		return false
	}
	return (ln[0] == '-' || ln[0] == '*' || ln[0] == '+') && ln[1] == ' '
}

// orderedLabel 返回行首 "N. " 形式的有序列表标记，非列表返回空。
func orderedLabel(ln string) string {
	i := 0
	for i < len(ln) && ln[i] >= '0' && ln[i] <= '9' {
		i++
	}
	if i == 0 || i > 3 || i >= len(ln) || ln[i] != '.' {
		return ""
	}
	if i+1 < len(ln) && ln[i+1] == ' ' {
		return ln[:i+2]
	}
	return ""
}
