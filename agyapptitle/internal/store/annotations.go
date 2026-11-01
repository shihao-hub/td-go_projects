package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var titlePbtxtRegex = regexp.MustCompile(`title\s*:\s*".*?"`)

// UpdateAnnotationFile 安全更新或创建 annotations/<id>.pbtxt 注解文件。
func UpdateAnnotationFile(annoDir, convID, newTitle string) error {
	if err := os.MkdirAll(annoDir, 0755); err != nil {
		return err
	}

	pbtxtPath := filepath.Join(annoDir, fmt.Sprintf("%s.pbtxt", convID))
	escapedTitle := strings.ReplaceAll(newTitle, `\`, `\\`)
	escapedTitle = strings.ReplaceAll(escapedTitle, `"`, `\"`)

	newField := fmt.Sprintf(`title:"%s"`, escapedTitle)

	if b, err := os.ReadFile(pbtxtPath); err == nil {
		content := string(b)
		var newContent string
		if titlePbtxtRegex.MatchString(content) {
			newContent = titlePbtxtRegex.ReplaceAllString(content, newField)
		} else {
			newContent = newField + "\n" + content
		}
		return os.WriteFile(pbtxtPath, []byte(newContent), 0644)
	}

	return os.WriteFile(pbtxtPath, []byte(newField+"\n"), 0644)
}
