package core

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

type DiffLine struct {
	Type    string `json:"type"` // "added", "removed", "same"
	Content string `json:"content"`
	LineOld int    `json:"line_old,omitempty"`
	LineNew int    `json:"line_new,omitempty"`
}

type DiffResult struct {
	IsBinary        bool       `json:"is_binary"`
	SourcePath      string     `json:"source_path"`
	SnapshotPath    string     `json:"snapshot_path"`
	SourceSize      int64      `json:"source_size"`
	SnapshotSize    int64      `json:"snapshot_size"`
	SourceSHA256    string     `json:"source_sha256"`
	SnapshotSHA256  string     `json:"snapshot_sha256"`
	Identical       bool       `json:"identical"`
	Lines           []DiffLine `json:"lines,omitempty"`
	Summary         string     `json:"summary"`
}

// CompareSnapshotWithSource 比对快照与当前源文件
func (e *Engine) CompareSnapshotWithSource(snapshotID int64) (*DiffResult, error) {
	snap, err := e.store.GetSnapshotByID(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("查找快照失败: %w", err)
	}

	snapData, err := os.ReadFile(snap.FullNutstorePath)
	if err != nil {
		return nil, fmt.Errorf("读取快照文件失败: %w", err)
	}

	cleanSource := CleanPath(snap.SourcePath)
	var sourceData []byte
	sourceExists := true
	if data, err := os.ReadFile(cleanSource); err == nil {
		sourceData = data
	} else {
		sourceExists = false
	}

	sourceHash, sourceSize, _ := CalculateFileHash(cleanSource)

	result := &DiffResult{
		SourcePath:     cleanSource,
		SnapshotPath:   snap.FullNutstorePath,
		SourceSize:     sourceSize,
		SnapshotSize:   int64(len(snapData)),
		SourceSHA256:   sourceHash,
		SnapshotSHA256: snap.SHA256,
	}

	if !sourceExists {
		result.Summary = "源文件当前在本地不存在"
		return result, nil
	}

	if bytes.Equal(sourceData, snapData) {
		result.Identical = true
		result.Summary = "内容完全一致"
		return result, nil
	}

	// 检查是否为二进制
	if !isText(snapData) || !isText(sourceData) {
		result.IsBinary = true
		result.Summary = "二进制文件，无法展示逐行文本差异"
		return result, nil
	}

	// 文本行比对
	oldLines := strings.Split(string(snapData), "\n")
	newLines := strings.Split(string(sourceData), "\n")
	result.Lines = computeSimpleDiff(oldLines, newLines)
	result.Summary = fmt.Sprintf("存在文本差异 (快照 %d 行 vs 当前 %d 行)", len(oldLines), len(newLines))
	return result, nil
}

func isText(b []byte) bool {
	if len(b) > 1024 {
		b = b[:1024]
	}
	if bytes.IndexByte(b, 0) != -1 {
		return false
	}
	return utf8.Valid(b)
}

func computeSimpleDiff(oldLines, newLines []string) []DiffLine {
	var diff []DiffLine
	i, j := 0, 0
	oldLen, newLen := len(oldLines), len(newLines)

	for i < oldLen && j < newLen {
		if oldLines[i] == newLines[j] {
			diff = append(diff, DiffLine{
				Type:    "same",
				Content: oldLines[i],
				LineOld: i + 1,
				LineNew: j + 1,
			})
			i++
			j++
		} else {
			foundOldInNew := -1
			for k := j; k < newLen && k < j+5; k++ {
				if oldLines[i] == newLines[k] {
					foundOldInNew = k
					break
				}
			}

			if foundOldInNew != -1 {
				for k := j; k < foundOldInNew; k++ {
					diff = append(diff, DiffLine{
						Type:    "added",
						Content: newLines[k],
						LineNew: k + 1,
					})
				}
				j = foundOldInNew
			} else {
				diff = append(diff, DiffLine{
					Type:    "removed",
					Content: oldLines[i],
					LineOld: i + 1,
				})
				i++
			}
		}
	}

	for ; i < oldLen; i++ {
		diff = append(diff, DiffLine{
			Type:    "removed",
			Content: oldLines[i],
			LineOld: i + 1,
		})
	}
	for ; j < newLen; j++ {
		diff = append(diff, DiffLine{
			Type:    "added",
			Content: newLines[j],
			LineNew: j + 1,
		})
	}

	return diff
}
