package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kbman/internal/parser"
)

var fileNameRegex = regexp.MustCompile(`^(?P<id>\d{3})_(?P<domain>[a-z]{2,12})_(?P<scope>[a-z0-9]{2,16})_(?P<type>fix|flow)_(?P<slug>.+)\.md$`)

type MigrationResult struct {
	TotalFiles   int      `json:"totalFiles"`
	UpdatedFiles int      `json:"updatedFiles"`
	SkippedFiles int      `json:"skippedFiles"`
	Errors       []string `json:"errors"`
}

// MigrateDir scans a KB directory and upgrades all notes with standardized
// temporal metadata. Existing frontmatter bytes are preserved verbatim; only
// missing fields are appended at the end of the block.
func MigrateDir(dirPath string, force bool) (*MigrationResult, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read dir failed: %w", err)
	}

	res := &MigrationResult{Errors: []string{}}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "SKILL.md" || e.Name() == "_SKILL.md" {
			continue
		}

		res.TotalFiles++
		filePath := filepath.Join(dirPath, e.Name())

		match := fileNameRegex.FindStringSubmatch(e.Name())
		if match == nil {
			res.SkippedFiles++
			continue
		}

		id, domain, scope, entryType, slug := match[1], match[2], match[3], match[4], match[5]

		raw, err := os.ReadFile(filePath)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: read error: %v", e.Name(), err))
			continue
		}

		doc, err := parser.Parse(raw)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: parse error: %v", e.Name(), err))
			continue
		}

		if !force && doc.HasKey("id") && doc.HasKey("status") && doc.HasKey("last_verified") {
			res.SkippedFiles++
			continue
		}

		fields := []struct{ key, value string }{
			{"id", id},
			{"domain", domain},
			{"scope", scope},
			{"type", entryType},
			{"slug", slug},
			{"status", "verified"},
			{"created", "2026-09-22"},
			{"last_verified", time.Now().Format("2006-01-02")},
		}

		for _, f := range fields {
			if force || !doc.HasKey(f.key) {
				doc.SetScalar(f.key, f.value)
			}
		}

		if force || !doc.HasKey("compatibility") {
			doc.SetBlock("compatibility", []string{
				"compatibility:",
				"  os: [\"windows\"]",
			})
		}

		if force || !doc.HasKey("invocation") {
			doc.SetBlock("invocation", []string{
				"invocation:",
				"  auto_trigger: true",
			})
		}

		updatedBytes, err := doc.Render()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: render error: %v", e.Name(), err))
			continue
		}

		if err := writeFileAtomic(filePath, updatedBytes, 0644); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: write error: %v", e.Name(), err))
			continue
		}

		res.UpdatedFiles++
	}

	return res, nil
}
