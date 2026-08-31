package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// token 类型
type tokenKind int

const (
	tokNumber tokenKind = iota
	tokOp              // + - * / %
	tokLParen          // (
	tokRParen          // )
	tokEnd             // 输入结束
)

type token struct {
	kind  tokenKind
	num   float64 // tokNumber 时有效
	op    byte    // tokOp 时有效
}

// 词法分析：把输入字符串拆成 token 列片
func tokenize(input string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(input) {
		c := input[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c >= '0' && c <= '9' || c == '.':
			start := i
			dotCount := 0
			for i < len(input) && (input[i] >= '0' && input[i] <= '9' || input[i] == '.') {
				if input[i] == '.' {
					dotCount++
				}
				i++
			}
			if dotCount > 1 {
				return nil, fmt.Errorf("数字 %q 中包含多个小数点", input[start:i])
			}
			var num float64
			if _, err := fmt.Sscanf(input[start:i], "%g", &num); err != nil {
				return nil, fmt.Errorf("无法解析数字 %q", input[start:i])
			}
			tokens = append(tokens, token{kind: tokNumber, num: num})
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '%':
			tokens = append(tokens, token{kind: tokOp, op: c})
			i++
		case c == '(':
			tokens = append(tokens, token{kind: tokLParen})
			i++
		case c == ')':
			tokens = append(tokens, token{kind: tokRParen})
			i++
		default:
			return nil, fmt.Errorf("无法识别的字符 %q", string(c))
		}
	}
	tokens = append(tokens, token{kind: tokEnd})
	return tokens, nil
}

// parser 持有 token 流的递归下降解析器
//
// 文法（优先级从低到高）：
//   expr   := term (('+' | '-') term)*
//   term   := factor (('*' | '/' | '%') factor)*
//   factor := '-' factor | '(' expr ')' | number
type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) next() token { t := p.tokens[p.pos]; p.pos++; return t }

func (p *parser) parseExpr() (float64, error) {
	val, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		t := p.peek()
		if t.kind != tokOp || (t.op != '+' && t.op != '-') {
			return val, nil
		}
		p.next()
		rhs, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if t.op == '+' {
			val += rhs
		} else {
			val -= rhs
		}
	}
}

func (p *parser) parseTerm() (float64, error) {
	val, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		t := p.peek()
		if t.kind != tokOp || (t.op != '*' && t.op != '/' && t.op != '%') {
			return val, nil
		}
		p.next()
		rhs, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		switch t.op {
		case '*':
			val *= rhs
		case '/':
			if rhs == 0 {
				return 0, fmt.Errorf("除数为零")
			}
			val /= rhs
		case '%':
			if rhs == 0 {
				return 0, fmt.Errorf("模数为零")
			}
			val = float64(int64(val) % int64(rhs))
		}
	}
}

func (p *parser) parseFactor() (float64, error) {
	t := p.next()
	switch {
	case t.kind == tokOp && t.op == '-':
		v, err := p.parseFactor()
		return -v, err
	case t.kind == tokOp && t.op == '+':
		return p.parseFactor()
	case t.kind == tokNumber:
		return t.num, nil
	case t.kind == tokLParen:
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.next().kind != tokRParen {
			return 0, fmt.Errorf("缺少右括号")
		}
		return v, nil
	case t.kind == tokEnd:
		return 0, fmt.Errorf("表达式不完整")
	default:
		return 0, fmt.Errorf("此处不应出现 %q", string(t.op))
	}
}

// evaluate 解析并计算表达式
func evaluate(input string) (float64, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return 0, err
	}
	p := &parser{tokens: tokens}
	val, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.peek().kind != tokEnd {
		return 0, fmt.Errorf("表达式末尾有多余内容")
	}
	return val, nil
}

// formatResult 把结果格式化为干净的字符串
func formatResult(v float64) string {
	if v == float64(int64(v)) && v < 1e15 && v > -1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

// ANSI 颜色/样式
const (
	styleReset  = "\x1b[0m"
	styleBold   = "\x1b[1m"
	styleDim    = "\x1b[2m"
	styleRed    = "\x1b[31m"
	styleGreen  = "\x1b[32m"
	styleYellow = "\x1b[33m"
	styleCyan   = "\x1b[36m"
	styleWhite  = "\x1b[37m"
)

// displayWidth 计算字符串在终端中的显示宽度（中日韩字符按 2 列计）
// 注意：含 ANSI 转义序列时按 0 宽计算
func displayWidth(s string) int {
	w := 0
	inEscape := false
	for _, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if r > 0x2E7F { // CJK 及全角字符范围
			w += 2
		} else {
			w++
		}
	}
	return w
}

// boxLine 打印一行带左右边框的内容，width 为内容区宽度
func boxLine(width int, format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	pad := width - displayWidth(content)
	if pad < 0 {
		pad = 0
	}
	fmt.Printf("  %s│%s %s %s%s│%s\n", styleCyan, styleReset, content, strings.Repeat(" ", pad), styleCyan, styleReset)
}

func printBoxTop(width int) {
	fmt.Printf("  %s╭", styleCyan)
	for i := 0; i < width+2; i++ {
		fmt.Print("─")
	}
	fmt.Printf("╮%s\n", styleReset)
}

func printBoxBottom(width int) {
	fmt.Printf("  %s╰", styleCyan)
	for i := 0; i < width+2; i++ {
		fmt.Print("─")
	}
	fmt.Printf("╯%s\n", styleReset)
}

func printBanner() {
	banner := styleYellow + styleBold + `
   ____                        ____                            _
  / ___|___  _ __ _ __   ___ _/ ___| __ _ _ __ ___   ___ _ __ | |_
 | |   / _ \| '__| '_ \ / _ \ |___ \ / _` + "`" + ` | '_ ` + "`" + `_ \ / _ \ '_ \| __|
 | |__| (_) | |  | | | |  __/ ___) | (_| | | | | | |  __/ | | | |_
  \____\___/|_|  |_| |_|\___|____/ \__,_|_| |_| |_|\___|_| |_|\__|
` + styleReset
	fmt.Println(banner)
	fmt.Printf("  %s%s交互式终端计算器%s  %s• %s支持 + - * / %%、括号、小数、一元负号%s\n",
		styleBold, styleWhite, styleReset, styleDim, styleReset, styleDim)
	fmt.Printf("  %s输入 %shelp%s 查看帮助，%squit%s 退出%s\n",
		styleDim, styleGreen, styleDim, styleGreen, styleDim, styleReset)
}

func printHelp() {
	const w = 46
	fmt.Println()
	printBoxTop(w)
	boxLine(w, "%s%s帮助 / HELP%s", styleBold, styleYellow, styleReset)
	boxLine(w, "")
	boxLine(w, "%s命令%s", styleCyan, styleReset)
	boxLine(w, "  help / h        显示本帮助")
	boxLine(w, "  quit / exit / q 退出计算器")
	boxLine(w, "%s支持的运算%s", styleCyan, styleReset)
	boxLine(w, "  加减乘除模      +  -  *  /  %%")
	boxLine(w, "  括号            (  )")
	boxLine(w, "  一元正负号      -3 + 5")
	boxLine(w, "  小数            3.14 * 2")
	boxLine(w, "%s示例%s", styleCyan, styleReset)
	boxLine(w, "  1 + 2 * 3       %s= 7%s", styleGreen, styleReset)
	boxLine(w, "  (1 + 2) * 3     %s= 9%s", styleGreen, styleReset)
	boxLine(w, "  10 / 4          %s= 2.5%s", styleGreen, styleReset)
	boxLine(w, "  -3 + 5          %s= 2%s", styleGreen, styleReset)
	printBoxBottom(w)
	fmt.Println()
}

// printResult 用面板展示算式和结果
func printResult(expr, result string) {
	const w = 46
	printBoxTop(w)
	boxLine(w, "%s%s%s", styleDim, expr, styleReset)
	boxLine(w, "%s= %s%s%s", styleBold, styleGreen, result, styleReset)
	printBoxBottom(w)
}

// printError 用红色面板展示错误
func printError(expr string, err error) {
	const w = 46
	printBoxTop(w)
	boxLine(w, "%s%s%s", styleDim, expr, styleReset)
	boxLine(w, "%s✗ %v%s", styleRed, err, styleReset)
	printBoxBottom(w)
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	printBanner()
	for {
		fmt.Printf("\n%s%s❯%s ", styleBold, styleCyan, styleReset)
		if !scanner.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch strings.ToLower(line) {
		case "quit", "exit", "q":
			fmt.Printf("\n  %s再见! 👋%s\n", styleYellow, styleReset)
			return
		case "help", "h":
			printHelp()
			continue
		}
		result, err := evaluate(line)
		if err != nil {
			printError(line, err)
			continue
		}
		printResult(line, formatResult(result))
	}
}
