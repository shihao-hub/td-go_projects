// history.go：采样历史读取用例（GUI 历史趋势曲线数据源）。
// 数据来自 store 落盘的 samples-YYYY-MM.jsonl（上游 data 原文），
// 行解析失败跳过（历史文件容忍坏行），不因个别行阻断整段曲线。
package service

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sort"
	"time"

	"glmquotawatch-gui/internal/api"
	"glmquotawatch-gui/internal/quota"
)

// maxHistoryFileBytes 单个历史文件读取上限，防异常膨胀文件拖垮进程。
const maxHistoryFileBytes = 8 << 20 // 8 MiB

// HistoryPoint 历史曲线上的一个点：采样时刻 + 窗口百分比。
type HistoryPoint struct {
	Ts  time.Time `json:"ts"`
	Pct int64     `json:"pct"`
}

// ReadHistory 读取自 since 起指定窗口的百分比历史（按时间升序）。
// windowKey 为窗口稳定键（Type:Unit:Number，如 "TOKENS_LIMIT:3:5"）；
// 无数据返回空切片（调用方展示空态）。
func (s *Service) ReadHistory(windowKey string, since time.Time) ([]HistoryPoint, error) {
	files, err := s.st.ListSampleFiles(since)
	if err != nil {
		return nil, errf("internal", "列出采样历史失败: %v", err)
	}
	points := make([]HistoryPoint, 0, 256)
	for _, path := range files {
		pts, rerr := parseHistoryFile(path, windowKey, since)
		if rerr != nil {
			// 单文件失败（被并发写/权限等）跳过，不阻断其余文件
			continue
		}
		points = append(points, pts...)
	}
	// 文件名升序、文件内追加序即时间升序；此处整体保险一次
	sort.Slice(points, func(i, j int) bool { return points[i].Ts.Before(points[j].Ts) })
	return points, nil
}

// parseHistoryFile 解析单个 samples JSONL 文件，收集匹配窗口的样本点。
func parseHistoryFile(path, windowKey string, since time.Time) ([]HistoryPoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := io.LimitReader(f, maxHistoryFileBytes)

	var out []HistoryPoint
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20) // 单行上限 1 MiB
	for scanner.Scan() {
		var line struct {
			Ts   string          `json:"ts"`
			Data json.RawMessage `json:"data"`
		}
		if jerr := json.Unmarshal(scanner.Bytes(), &line); jerr != nil {
			continue // 坏行跳过
		}
		ts, terr := time.Parse(time.RFC3339, line.Ts)
		if terr != nil || ts.Before(since) {
			continue
		}
		var usage api.Usage
		if jerr := json.Unmarshal(line.Data, &usage); jerr != nil {
			continue
		}
		for _, l := range usage.TokensLimits() {
			if l.Key() == windowKey && l.Percentage != nil {
				out = append(out, HistoryPoint{Ts: ts, Pct: *l.Percentage})
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// HistoryWindow 历史窗口条目（供前端窗口选择器）。
type HistoryWindow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// HistoryWindows 返回近 7 天采样数据中实际出现过的窗口集合；无数据返回空切片。
func (s *Service) HistoryWindows() ([]HistoryWindow, error) {
	files, err := s.st.ListSampleFiles(time.Now().AddDate(0, 0, -7))
	if err != nil {
		return nil, errf("internal", "列出采样历史失败: %v", err)
	}
	seen := map[string]bool{}
	out := []HistoryWindow{}
	for _, path := range files {
		ws, rerr := scanWindowKeys(path)
		if rerr != nil {
			continue
		}
		for _, w := range ws {
			if !seen[w.Key] {
				seen[w.Key] = true
				out = append(out, w)
			}
		}
	}
	return out, nil
}

// scanWindowKeys 扫描单个历史文件中出现的全部 TOKENS_LIMIT 窗口键。
func scanWindowKeys(path string) ([]HistoryWindow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := io.LimitReader(f, maxHistoryFileBytes)

	seen := map[string]bool{}
	var out []HistoryWindow
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var line struct {
			Data json.RawMessage `json:"data"`
		}
		if jerr := json.Unmarshal(scanner.Bytes(), &line); jerr != nil {
			continue
		}
		var usage api.Usage
		if jerr := json.Unmarshal(line.Data, &usage); jerr != nil {
			continue
		}
		for _, l := range usage.TokensLimits() {
			k := l.Key()
			if !seen[k] {
				seen[k] = true
				out = append(out, HistoryWindow{Key: k, Label: quota.WindowLabel(l)})
			}
		}
	}
	return out, scanner.Err()
}
