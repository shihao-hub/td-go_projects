package runner

import (
	"sort"
	"strings"
)

// suggest 从已注册名中挑出与 input 最相似的候选（前缀 > 子串 > 编辑距离）。
// 排序规则：rank 升序，同 rank 按字典序；最多返回 limit 个。
// rank 含义：0=前缀匹配 1=子串包含 2+d=编辑距离 d
func suggest(input string, names []string, limit int) []string {
	if input == "" || len(names) == 0 || limit <= 0 {
		return nil
	}
	ranked := make(map[string]int, len(names))
	for _, n := range names {
		if n == input {
			continue // 完全一致不会出现在 not_found 场景
		}
		switch {
		case strings.HasPrefix(n, input):
			ranked[n] = 0
		case strings.Contains(n, input):
			ranked[n] = 1
		}
	}
	// 编辑距离补充：短输入（<3 字符）噪声大，只做前缀/子串
	if len(input) >= 3 {
		maxDist := 1
		if len(input) >= 6 {
			maxDist = 2
		}
		for _, n := range names {
			if _, ok := ranked[n]; ok || n == input {
				continue
			}
			if d := levenshtein(input, n); d <= maxDist {
				ranked[n] = 2 + d
			}
		}
	}
	if len(ranked) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranked))
	for n := range ranked {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := ranked[out[i]], ranked[out[j]]
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// levenshtein 经典两行 DP 编辑距离（rune 级，兼容非 ASCII 名）
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
