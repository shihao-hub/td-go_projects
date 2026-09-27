package service

import (
	"path/filepath"
	"sort"
	"strings"

	"sourcecount/internal/contract"
	"sourcecount/internal/scanner"
)

func aggregate(roots []string, configPath string, results []scanner.Result) contract.ScanReport {
	groups := make(map[string]*contract.ExtensionGroup)
	report := contract.ScanReport{
		Roots:      append([]string(nil), roots...),
		ConfigPath: configPath,
		Groups:     make([]contract.ExtensionGroup, 0),
		Errors:     make([]contract.ScanError, 0),
	}
	for _, result := range results {
		if result.Error != nil {
			report.Errors = append(report.Errors, *result.Error)
			continue
		}
		group := groups[result.Extension]
		if group == nil {
			group = &contract.ExtensionGroup{Extension: result.Extension}
			groups[result.Extension] = group
		}
		group.Text.Files += result.Text.Files
		group.Text.Lines += result.Text.Lines
		group.Text.Chars += result.Text.Chars
		group.Binary.Files += result.Binary.Files
		group.Binary.Bytes += result.Binary.Bytes
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "<none>" {
			return false
		}
		if keys[j] == "<none>" {
			return true
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		group := *groups[key]
		report.Groups = append(report.Groups, group)
		report.Totals.Text.Files += group.Text.Files
		report.Totals.Text.Lines += group.Text.Lines
		report.Totals.Text.Chars += group.Text.Chars
		report.Totals.Binary.Files += group.Binary.Files
		report.Totals.Binary.Bytes += group.Binary.Bytes
	}
	sort.Slice(report.Errors, func(i, j int) bool {
		return pathKey(report.Errors[i].Path) < pathKey(report.Errors[j].Path)
	})
	return report
}

func pathKey(value string) string {
	return strings.ToLower(filepath.Clean(value))
}
