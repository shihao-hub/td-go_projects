package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

var effortOrder = map[string]int{"max": 0, "high": 1, "medium": 2, "low": 3, "default": 4}

func effortRank(v string) int {
	if n, ok := effortOrder[v]; ok {
		return n
	}
	if v == "" {
		return 5
	}
	return 6
}

// effortSortKey: 「default→max」这类合并展示取 → 后面的真实档位参与排序。
func effortSortKey(v string) string {
	if i := strings.Index(v, "→"); i >= 0 {
		return v[i+len("→"):]
	}
	return v
}

func effortLess(a, b string) bool {
	ka, kb := effortSortKey(a), effortSortKey(b)
	if effortRank(ka) != effortRank(kb) {
		return effortRank(ka) < effortRank(kb)
	}
	return a < b
}

// resolveEffort 合并配置档位：variant 为 default/缺失且配置里写死了 effort
// （options.reasoningEffort / effort）时展示为「default→max」；
// 真实档位变体（max/low 等）本身就是档位，原样返回。
func resolveEffort(cfg effortConfig, provider, model, variant string) string {
	if variant != "" && variant != "default" {
		return variant
	}
	if eff := cfg.lookup(provider, model); eff != "" {
		return "default→" + eff
	}
	if variant == "" {
		return "-"
	}
	return variant
}

func render(w io.Writer, snap *dataSnapshot) {
	printHeader(w, snap)
	printSummary(w, snap)
}

func printHeader(w io.Writer, snap *dataSnapshot) {
	home, _ := os.UserHomeDir()
	dbDisp := snap.DBPath
	if home != "" && strings.HasPrefix(snap.DBPath, home) {
		dbDisp = "~" + strings.TrimPrefix(snap.DBPath, home)
	}
	fmt.Fprintf(w, "ocstat %s · opencode 会话模型/思考档位统计\n", version)
	fmt.Fprintf(w, "db: %s (%s, 修改于 %s)", dbDisp, humanSize(snap.DBSize), snap.DBMod.Format("01-02 15:04"))
	if snap.UsingSnapshot {
		fmt.Fprint(w, " · [快照兜底]")
	}
	fmt.Fprintln(w)
	integrity := "完整"
	if snap.Mode == modeBasic {
		integrity = "降级"
	}
	fmt.Fprintf(w, "会话 %d", len(snap.Sessions))
	if snap.Mode == modeFull {
		fmt.Fprintf(w, "（无实际消息 %d，有启动模型 %d）", snap.NoMsgCount, snap.StartupCount)
	}
	fmt.Fprintf(w, " · opencode 版本 %s · 数据 %s", versionRange(snap.Versions), integrity)
	if snap.CfgMerged {
		fmt.Fprint(w, " · 档位已合并配置")
	}
	fmt.Fprintf(w, " · 生成于 %s\n", snap.Generated.Format("01-02 15:04:05"))
	if snap.Mode == modeBasic {
		fmt.Fprintf(w, "⚠ %s\n", snap.Note)
	}
	fmt.Fprintln(w)
}

func printSummary(w io.Writer, snap *dataSnapshot) {
	title := "启动模型 × 档位"
	if snap.Mode == modeBasic {
		title = "当前模型 × 档位（启动档位不可用）"
	}
	fmt.Fprintf(w, "── %s（按会话数降序，档位 max>high>medium>low>default）──\n", title)

	type gkey struct{ p, m string }
	type grow struct {
		effort   string
		sessions int
	}
	groups := map[gkey][]grow{}
	totals := map[gkey]int{}
	var order []gkey
	total := 0
	for _, st := range snap.Sessions {
		var m *modelSel
		if snap.Mode == modeFull {
			m = st.Startup
		} else {
			m = st.Current
		}
		if m == nil {
			continue
		}
		k := gkey{p: m.Provider, m: m.ID}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		v := resolveEffort(snap.EffortCfg, m.Provider, m.ID, m.Variant)
		found := false
		for i := range groups[k] {
			if groups[k][i].effort == v {
				groups[k][i].sessions++
				found = true
				break
			}
		}
		if !found {
			groups[k] = append(groups[k], grow{effort: v, sessions: 1})
		}
		totals[k]++
		total++
	}
	if total == 0 {
		fmt.Fprintln(w, "（暂无数据）")
		fmt.Fprintln(w)
		return
	}

	sort.Slice(order, func(i, j int) bool {
		if totals[order[i]] != totals[order[j]] {
			return totals[order[i]] > totals[order[j]]
		}
		return order[i].p+"/"+order[i].m < order[j].p+"/"+order[j].m
	})

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tMODEL\tEFFORT\t会话\t占比")
	for _, k := range order {
		rows := groups[k]
		sort.SliceStable(rows, func(i, j int) bool { return effortLess(rows[i].effort, rows[j].effort) })
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%.1f%%\n", k.p, k.m, r.effort, r.sessions, float64(r.sessions)*100/float64(total))
		}
	}
	fmt.Fprintf(tw, "合计\t\t\t%d\t100%%\n", total)
	tw.Flush()
	fmt.Fprintln(w)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func versionRange(vs []string) string {
	if len(vs) == 0 {
		return "-"
	}
	if len(vs) == 1 {
		return vs[0]
	}
	return vs[0] + "~" + vs[len(vs)-1]
}
