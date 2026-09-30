package core

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

// DocumentInfo 表示在磁盘上扫描到的文档信息
type DocumentInfo struct {
	Type         string `json:"type"`          // "plan" 或 "spec"
	Path         string `json:"path"`          // 相对路径，如 docs/plans/01-test.md
	Registered   bool   `json:"registered"`    // 是否已在投影库登记
	Status       string `json:"status"`        // 登记的状态 JSON 字符串，未登记为空
	StateSummary string `json:"state_summary"` // 提取的状态简要（如 "completed", "in_progress", "unregistered"）
}

// ScanReport 扫描对齐比对总报告
type ScanReport struct {
	TotalDiscovered int            `json:"total_discovered"`
	TotalRegistered int            `json:"total_registered"`
	Unregistered    []DocumentInfo `json:"unregistered"` // 存在但未在 SQL 中登记
	Dangling        []Item         `json:"dangling"`     // SQL 中存在但磁盘文件已删除
	Aligned         []DocumentInfo `json:"aligned"`      // 存在且已正常登记
}

// Scanner 负责扫描磁盘目录与 SQLite 投影并生成对齐状态
type Scanner struct {
	rootDir string
}

// NewScanner 创建对齐扫描器
func NewScanner(rootDir string) *Scanner {
	return &Scanner{rootDir: rootDir}
}

// Scan 对比磁盘中的 **/plans/** 和 **/specs/** 与 store 记录
func (sc *Scanner) Scan(store *Store) (*ScanReport, error) {
	report := &ScanReport{
		Unregistered: []DocumentInfo{},
		Dangling:     []Item{},
		Aligned:      []DocumentInfo{},
	}

	// 1. 扫描磁盘上全部 markdown 文件
	discoveredMap := make(map[string]DocumentInfo)
	err := filepath.WalkDir(sc.rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读目录
		}
		if d.IsDir() {
			name := d.Name()
			// 忽略常见依赖与隐藏目录
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".gemini" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		rel, err := filepath.Rel(sc.rootDir, path)
		if err != nil {
			return nil
		}
		cleanRel := filepath.ToSlash(filepath.Clean(rel))

		// 检查路径是否命中 **/plans/** 或 **/specs/**
		isPlan := strings.Contains("/"+cleanRel+"/", "/plans/")
		isSpec := strings.Contains("/"+cleanRel+"/", "/specs/")

		if !isPlan && !isSpec {
			return nil
		}

		itemType := "plan"
		if isSpec {
			itemType = "spec"
		}

		discoveredMap[cleanRel] = DocumentInfo{
			Type:         itemType,
			Path:         cleanRel,
			Registered:   false,
			StateSummary: "unregistered",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	report.TotalDiscovered = len(discoveredMap)

	// 2. 从 Store 中拉取当前所有已记录项
	items, err := store.GetAllItems()
	if err != nil {
		return nil, err
	}
	report.TotalRegistered = len(items)

	dbItemMap := make(map[string]Item)
	for _, it := range items {
		dbItemMap[it.Path] = it
	}

	// 3. 计算已对齐与未登记文档
	for p, doc := range discoveredMap {
		if it, exists := dbItemMap[p]; exists {
			doc.Registered = true
			doc.Status = it.Status
			doc.StateSummary = extractState(it.Status)
			report.Aligned = append(report.Aligned, doc)
			delete(dbItemMap, p)
		} else {
			report.Unregistered = append(report.Unregistered, doc)
		}
	}

	// 4. dbItemMap 中剩余的项目即为“磁盘不存在但数据库有”的悬空记录
	for _, it := range dbItemMap {
		report.Dangling = append(report.Dangling, it)
	}

	return report, nil
}

// 辅助提取 JSON 内部的 state 或 status 字段
func extractState(statusJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(statusJSON), &m); err != nil {
		return "unknown"
	}
	if s, ok := m["state"].(string); ok && s != "" {
		return s
	}
	if s, ok := m["status"].(string); ok && s != "" {
		return s
	}
	return "registered"
}
