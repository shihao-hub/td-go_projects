package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kbman/internal/parser"
)

type CheckReport struct {
	ID                string  `json:"id"`
	FilePath          string  `json:"filePath"`
	Status            string  `json:"status"`
	LastVerified      string  `json:"lastVerified"`
	SupersededBy      *string `json:"supersededBy,omitempty"`
	DeprecationReason *string `json:"deprecationReason,omitempty"`
	Falsification     any     `json:"falsificationSignals,omitempty"`
	CanExecute        bool    `json:"canExecute"`
	Warning           string  `json:"warning,omitempty"`
}

// FindEntry locates a KB entry by ID or filename in dirPath.
func FindEntry(dirPath, target string) (string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == target || strings.HasPrefix(e.Name(), target+"_") {
			return filepath.Join(dirPath, e.Name()), nil
		}
	}
	return "", fmt.Errorf("entry [%s] not found in %s", target, dirPath)
}

// CheckEntry performs pre-execution validation on a KB note.
func CheckEntry(filePath string) (*CheckReport, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	doc, err := parser.Parse(raw)
	if err != nil {
		return nil, err
	}

	rep := &CheckReport{
		ID:                doc.Meta.ID,
		FilePath:          filePath,
		Status:            doc.Meta.Status,
		LastVerified:      doc.Meta.LastVerified,
		SupersededBy:      doc.Meta.SupersededBy,
		DeprecationReason: doc.Meta.DeprecationReason,
		Falsification:     doc.Meta.FalsificationSignals,
		CanExecute:        true,
	}

	switch doc.Meta.Status {
	case "deprecated":
		rep.CanExecute = false
		reason := "无详细原因"
		if doc.Meta.DeprecationReason != nil {
			reason = *doc.Meta.DeprecationReason
		}
		super := "无替代条目"
		if doc.Meta.SupersededBy != nil {
			super = *doc.Meta.SupersededBy
		}
		rep.Warning = fmt.Sprintf("🛑 警告：该方案已废弃！原因: %s。请改用: %s", reason, super)
	case "suspect":
		rep.Warning = "⚠️ 提示：该方案近期存在冲突或环境存疑，执行前必须通过只读命令先行探测验证。"
	case "draft":
		rep.Warning = "ℹ️ 提示：该方案为暂行草案，尚未经过多场景实测。"
	}

	return rep, nil
}

// UpdateEntry modifies status and appends a reflection log atomically.
func UpdateEntry(filePath, status, reason, appendLog string) error {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	doc, err := parser.Parse(raw)
	if err != nil {
		return err
	}

	if status != "" {
		doc.SetScalar("status", status)
	}
	if reason != "" {
		doc.SetScalar("deprecation_reason", reason)
	}
	doc.SetScalar("last_verified", time.Now().Format("2006-01-02"))

	if appendLog != "" {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		logEntry := fmt.Sprintf("\n\n---\n\n### 运行时自愈与踩坑反馈 (%s)\n%s\n", timestamp, appendLog)
		doc.Content += logEntry
	}

	updatedBytes, err := doc.Render()
	if err != nil {
		return err
	}

	return writeFileAtomic(filePath, updatedBytes, 0644)
}
