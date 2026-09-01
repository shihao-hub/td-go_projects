package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
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

func effortLess(a, b string) bool {
	if effortRank(a) != effortRank(b) {
		return effortRank(a) < effortRank(b)
	}
	return a < b
}

func render(w io.Writer, snap *dataSnapshot, opt options) {
	printHeader(w, snap)
	printSummary(w, snap)
	printDetail(w, snap, opt)
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
	fmt.Fprintf(w, " · opencode 版本 %s · 数据 %s · 生成于 %s\n",
		versionRange(snap.Versions), integrity, snap.Generated.Format("01-02 15:04:05"))
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
		v := m.effort()
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

func printDetail(w io.Writer, snap *dataSnapshot, opt options) {
	list := snap.Sessions
	if opt.switched {
		var filtered []*sessionStat
		for _, st := range list {
			if st.Switched {
				filtered = append(filtered, st)
			}
		}
		list = filtered
	}
	if !opt.showAll && opt.limit > 0 && len(list) > opt.limit {
		list = list[:opt.limit]
	}

	scope := "最近 " + strconv.Itoa(opt.limit)
	if opt.showAll || opt.limit <= 0 {
		scope = "全部"
	}
	if opt.switched {
		scope += "，仅切换过的"
	}
	fmt.Fprintf(w, "── 会话明细（%s，⇄ = 切换过模型/档位）──\n", scope)
	if len(list) == 0 {
		fmt.Fprintln(w, "（无匹配会话）")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "时间\t目录\tAGENT\t启动模型\t启动档\t当前档\t消息\t⇄\t用过的模型")
	for _, st := range list {
		mark := ""
		if st.Switched {
			mark = "⇄"
		}
		msgs := "-"
		if snap.Mode == modeFull {
			msgs = strconv.Itoa(st.MsgCount)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			time.UnixMilli(st.Created).Format("01-02 15:04"),
			shortDir(st.Dir),
			orDash(st.Agent),
			st.Startup.name(),
			st.Startup.effort(),
			st.Current.effort(),
			msgs,
			mark,
			formatUsed(st.Used),
		)
	}
	tw.Flush()
	if snap.Mode == modeFull {
		fmt.Fprintln(w, "注: opencode 内置模型（如 big-pickle 标题模型）不参与切换判定")
	}
}

func shortDir(dir string) string {
	if dir == "" {
		return "-"
	}
	d := strings.ReplaceAll(dir, "\\", "/")
	d = strings.TrimRight(d, "/")
	if i := strings.LastIndex(d, "/"); i >= 0 {
		d = d[i+1:]
	}
	if d == "" {
		return "/"
	}
	if len(d) > 18 {
		return d[:15] + "…"
	}
	return d
}

func formatUsed(used []modelSel) string {
	if len(used) == 0 {
		return "-"
	}
	type group struct {
		p, m string
		vars []string
	}
	var gs []*group
	idx := map[[2]string]*group{}
	for _, u := range used {
		k := [2]string{u.Provider, u.ID}
		if gr, ok := idx[k]; ok {
			if u.Variant != "" && !containsStr(gr.vars, u.Variant) {
				gr.vars = append(gr.vars, u.Variant)
			}
			continue
		}
		gr := &group{p: u.Provider, m: u.ID}
		if u.Variant != "" {
			gr.vars = append(gr.vars, u.Variant)
		}
		idx[k] = gr
		gs = append(gs, gr)
	}
	var parts []string
	for _, gr := range gs {
		s := gr.p + "/" + gr.m
		if len(gr.vars) > 0 {
			vs := append([]string(nil), gr.vars...)
			sort.SliceStable(vs, func(i, j int) bool { return effortLess(vs[i], vs[j]) })
			s += "(" + strings.Join(vs, "/") + ")"
		}
		parts = append(parts, s)
	}
	out := strings.Join(parts, "; ")
	if len(out) > 64 {
		r := []rune(out)
		return string(r[:61]) + "…"
	}
	return out
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
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
