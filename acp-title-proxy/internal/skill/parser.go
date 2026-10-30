package skill

import (
	"bufio"
	"strings"
	"unicode/utf8"
)

// ParseFrontmatter extracts name and description from a SKILL.md content.
func ParseFrontmatter(content string) (name, description string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return "", ""
	}

	// Must start with "---"
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

		// Handle multi-line description continuation
		if readingDesc {
			// Check indentation of current line
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

		// Match top-level keys
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

// CleanDescription cleans whitespace and limits description length without cutting UTF-8 runes.
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
