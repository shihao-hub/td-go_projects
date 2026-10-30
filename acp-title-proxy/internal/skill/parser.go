package skill

import (
	"bufio"
	"strings"
	"unicode/utf8"
)

// ParseFrontmatter 从 SKILL.md 内容中提取 name 与 description。
func ParseFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return "", ""
	}

	// 必须以 "---" 开头
	firstLine := strings.TrimSpace(lines[0])
	if firstLine != "---" {
		return "", ""
	}

	inFrontmatter := true
	var descLines []string
	readingDesc := false
	descIndent := -1

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" || trimmed == "..." {
			inFrontmatter = false
			break
		}

		if !inFrontmatter {
			break
		}

		// 处理多行 description 的续行
		if readingDesc {
			// 检查当前行的缩进
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if trimmed == "" {
				descLines = append(descLines, "")
				continue
			}

			if descIndent == -1 {
				descIndent = currentIndent
			}

			if currentIndent >= descIndent {
				descLines = append(descLines, strings.TrimSpace(line))
				continue
			} else {
				readingDesc = false
			}
		}

		// 匹配顶层键
		if strings.HasPrefix(trimmed, "name:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			name = strings.Trim(val, `"'`)
			readingDesc = false
		} else if strings.HasPrefix(trimmed, "description:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
			valClean := strings.Trim(val, `"'|>- `)
			if valClean != "" {
				descLines = append(descLines, valClean)
			}
			readingDesc = true
			descIndent = -1
		} else if strings.Contains(trimmed, ":") && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			readingDesc = false
		}
	}

	if len(descLines) > 0 {
		var cleanedWords []string
		for _, dl := range descLines {
			dl = strings.TrimSpace(dl)
			if dl != "" {
				cleanedWords = append(cleanedWords, dl)
			}
		}
		description = strings.Join(cleanedWords, " ")
		description = CleanDescription(description, 180)
	}

	return name, description
}

// CleanDescription 清理空白并限制描述长度，且不截断 UTF-8 字符。
func CleanDescription(desc string, maxRunes int) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(desc))
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		word := scanner.Text()
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(word)
	}
	res := sb.String()

	if maxRunes <= 0 {
		return res
	}

	runeCount := utf8.RuneCountInString(res)
	if runeCount > maxRunes {
		runes := []rune(res)
		return string(runes[:maxRunes]) + "…"
	}
	return res
}
