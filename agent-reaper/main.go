// agent-reaper: find and optionally kill idle AI-agent processes
// (opencode / claude code / codex) spawned by Zed, based on time since
// LAST USE (CPU/IO activity), not start time.
//
// Usage:
//   agent-reaper.exe                 interactive: show table, ask, kill stale
//   agent-reaper.exe -list           show table only
//   agent-reaper.exe -sample         silent: record activity snapshot to state (for Task Scheduler)
//   agent-reaper.exe -watch 300      keep sampling every 300s (add -auto to auto-kill)
//   agent-reaper.exe -hours 8        idle threshold (default 6h)
//   agent-reaper.exe -all            also target agents NOT spawned by Zed
//   agent-reaper.exe -install        register Task Scheduler task: sample every 5 min
//   agent-reaper.exe -uninstall      remove the Task Scheduler task
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const psQuery = `$ErrorActionPreference='SilentlyContinue'; Get-CimInstance Win32_Process | ForEach-Object { [pscustomobject]@{ Pid=[int]$_.ProcessId; Ppid=[int]$_.ParentProcessId; Name=$_.Name; Path=$_.ExecutablePath; Cmd=$_.CommandLine; Start=[int64]([DateTimeOffset]::new($_.CreationDate).ToUnixTimeSeconds()); Cpu=[int64]($_.KernelModeTime)+[int64]($_.UserModeTime); Rio=[int64]$_.ReadTransferCount; Wio=[int64]$_.WriteTransferCount; Ws=[int64]($_.WorkingSetSize/1MB) } } | ConvertTo-Json -Compress`

type psProc struct {
	Pid   int32  `json:"Pid"`
	Ppid  int32  `json:"Ppid"`
	Name  string `json:"Name"`
	Path  string `json:"Path"`
	Cmd   string `json:"Cmd"`
	Start int64  `json:"Start"`
	Cpu   int64  `json:"Cpu"` // cumulative, 100ns units
	Rio   int64  `json:"Rio"`
	Wio   int64  `json:"Wio"`
	Ws    int64  `json:"Ws"` // MB
}

type stRec struct {
	Kind, Via, Name, Path, Cmd string
	Start, FirstSeen, LastSeen, LastUsed int64
	Cpu, Rio, Wio int64
}

var stateDir string
var statePath string
var logPath string

func main() {
	hours := flag.Float64("hours", 6, "idle threshold in hours (time since last use)")
	list := flag.Bool("list", false, "show status table only")
	sample := flag.Bool("sample", false, "silent one-shot snapshot, save state (for scheduler)")
	watch := flag.Int("watch", 0, "seconds between samples; run continuously")
	auto := flag.Bool("auto", false, "with -watch: kill stale processes automatically")
	all := flag.Bool("all", false, "also consider agents NOT spawned by Zed")
	scopes := flag.String("scopes", "zed", "stale-kill scopes, comma-separated: zed,vscode,terminal,other")
	yes := flag.Bool("yes", false, "skip kill confirmation")
	install := flag.Bool("install", false, "register Task Scheduler sampling task (every 5 min)")
	uninstall := flag.Bool("uninstall", false, "remove Task Scheduler sampling task")
	flag.Parse()
	flagHours = *hours
	flagAll = *all
	for _, s := range strings.Split(*scopes, ",") {
		flagScopes[strings.TrimSpace(strings.ToLower(s))] = true
	}

	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		la = "."
	}
	stateDir = filepath.Join(la, "agent-reaper")
	statePath = filepath.Join(stateDir, "state.json")
	logPath = filepath.Join(stateDir, "reaper.log")
	_ = os.MkdirAll(stateDir, 0o755)

	switch {
	case *install:
		doInstall()
		return
	case *uninstall:
		doUninstall()
		return
	}

	if *sample {
		if _, _, err := collectAndRecord(); err != nil {
			fmt.Fprintln(os.Stderr, "sample error:", err)
			os.Exit(1)
		}
		return
	}

	if *watch > 0 {
		for {
			rows, stale, err := collectAndRecord()
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			} else {
				printTable(rows, *hours)
				if *auto && len(stale) > 0 {
					killAll(stale, true)
				}
			}
			time.Sleep(time.Duration(*watch) * time.Second)
		}
	}

	rows, stale, err := collectAndRecord()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		waitEnter(*list)
		os.Exit(1)
	}
	printTable(rows, *hours)

	if len(stale) > 0 && !*list {
		scope := "Zed-spawned"
		if *all {
			scope = "ALL"
		}
		fmt.Printf("\n%d stale agent process(es) idle for more than %.1fh (%s):\n", len(stale), *hours, scope)
		for _, r := range stale {
			fmt.Printf("  PID %d  %-9s via %-8s idle %.1fh\n", r.Pid, r.Kind, r.Via, r.IdleH)
		}
		if *yes || confirm("Kill them (whole process trees)?") {
			killAll(stale, false)
		}
	} else if len(stale) == 0 {
		fmt.Printf("\nNo agent has been idle for more than %.1fh. Nothing to do.\n", *hours)
	}
	waitEnter(*list)
}

type row struct {
	Pid   int32
	Kind  string
	Via   string
	Start time.Time
	AgeH  float64
	IdleH float64
	CpuS  int64
	WsMB  int64
	Stale bool
}

// collectAndRecord: query processes, classify targets, update state, return rows + stale list.
func collectAndRecord() ([]row, []row, error) {
	procs, err := query()
	if err != nil {
		return nil, nil, err
	}
	byPid := make(map[int32]psProc, len(procs))
	for _, p := range procs {
		byPid[p.Pid] = p
	}

	state := loadState()
	now := time.Now().Unix()
	seen := map[string]bool{}

	// first pass: classify everything; children of another agent get the parent's kind
	kindByPid := map[int32]string{}
	for _, p := range procs {
		if k := classify(p); k != "" {
			kindByPid[p.Pid] = k
		}
	}
	for pid, k := range kindByPid {
		base := strings.Split(k, "-")[0]
		p := byPid[pid]
		for i := 0; i < 4; i++ {
			par, ok := byPid[p.Ppid]
			if !ok {
				break
			}
			if pk, ok := kindByPid[par.Pid]; ok && strings.Split(pk, "-")[0] != base {
				k = strings.Split(pk, "-")[0] + "-tool"
				break
			}
			p = par
		}
		kindByPid[pid] = k
	}

	var rows []row
	for _, p := range procs {
		kind := kindByPid[p.Pid]
		if kind == "" {
			continue
		}
		via := viaOf(p, byPid)
		key := strconv.Itoa(int(p.Pid))
		rec, ok := state[key]
		if !ok || rec.Start != p.Start {
			rec = &stRec{Kind: kind, Via: via, Name: p.Name, Path: p.Path, Cmd: p.Cmd,
				Start: p.Start, FirstSeen: now, LastSeen: now, LastUsed: now,
				Cpu: p.Cpu, Rio: p.Rio, Wio: p.Wio}
		} else {
			cpuDelta := p.Cpu - rec.Cpu
			ioDelta := (p.Rio + p.Wio) - (rec.Rio + rec.Wio)
			if cpuDelta > 20_000_000 || ioDelta > 2*1024*1024 { // >2s CPU or >2MB IO since last sample
				rec.LastUsed = now
			}
			rec.Kind, rec.Via, rec.Name, rec.Path, rec.Cmd = kind, via, p.Name, p.Path, p.Cmd
			rec.LastSeen = now
			rec.Cpu, rec.Rio, rec.Wio = p.Cpu, p.Rio, p.Wio
		}
		state[key] = rec
		seen[key] = true

		rows = append(rows, row{Pid: p.Pid, Kind: kind, Via: via,
			Start: time.Unix(p.Start, 0), AgeH: hoursSince(p.Start, now),
			IdleH: hoursSince(rec.LastUsed, now), CpuS: p.Cpu / 10_000_000, WsMB: p.Ws,
			Stale: hoursSince(rec.LastUsed, now) > *threshold()})
	}
	purge(state, seen, now)
	saveState(state)

	var stale []row
	for i := range rows {
		if rows[i].Stale && scopeHit(rows[i].Via) {
			stale = append(stale, rows[i])
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].IdleH > rows[j].IdleH })
	return rows, stale, nil
}

func classify(p psProc) string {
	n := strings.ToLower(p.Name)
	pl := strings.ToLower(p.Path + " " + p.Cmd)
	switch {
	case n == "opencode.exe" || strings.Contains(pl, "opencode-ai") || strings.Contains(pl, `external_agents\registry\opencode`):
		return "opencode"
	case n == "claude.exe" || strings.Contains(pl, "claude-code") || strings.Contains(pl, "anthropic-ai"):
		return "claude"
	case n == "codex.exe" || strings.Contains(pl, "@openai/codex") || strings.Contains(pl, `openai\codex`) || strings.Contains(pl, "codex-rs"):
		return "codex"
	}
	return ""
}

func viaOf(p psProc, byPid map[int32]psProc) string {
	cur := p
	for i := 0; i < 12; i++ {
		if strings.EqualFold(cur.Name, "zed.exe") {
			return "zed"
		}
		par, ok := byPid[cur.Ppid]
		if !ok {
			break
		}
		cur = par
	}
	cur = p
	for i := 0; i < 12; i++ {
		if strings.EqualFold(cur.Name, "code.exe") {
			return "vscode"
		}
		par, ok := byPid[cur.Ppid]
		if !ok {
			break
		}
		cur = par
	}
	cur = p
	for i := 0; i < 12; i++ {
		n := strings.ToLower(cur.Name)
		if strings.Contains(n, "windowsterminal") || n == "cmd.exe" || n == "conhost.exe" ||
			strings.Contains(n, "wezterm") || strings.Contains(n, "alacritty") || strings.Contains(n, "ghostty") {
			return "terminal"
		}
		par, ok := byPid[cur.Ppid]
		if !ok {
			break
		}
		cur = par
	}
	return "other"
}

func query() ([]psProc, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", psQuery).Output()
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "{") {
		s = "[" + s + "]"
	}
	var procs []psProc
	if err := json.Unmarshal([]byte(s), &procs); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return procs, nil
}

func loadState() map[string]*stRec {
	m := map[string]*stRec{}
	b, err := os.ReadFile(statePath)
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func saveState(m map[string]*stRec) {
	b, _ := json.MarshalIndent(m, "", " ")
	_ = os.WriteFile(statePath, b, 0o644)
}

// purge drops records whose process no longer exists or wasn't seen for 24h.
func purge(state map[string]*stRec, seen map[string]bool, now int64) {
	for k, r := range state {
		if !seen[k] && now-r.LastSeen > 24*3600 {
			delete(state, k)
		}
	}
}

func killAll(stale []row, silent bool) {
	for _, r := range stale {
		out, err := exec.Command("taskkill", "/PID", strconv.Itoa(int(r.Pid)), "/T", "/F").CombinedOutput()
		status := "killed"
		if err != nil {
			status = "FAILED: " + strings.TrimSpace(string(out))
		}
		logLine(fmt.Sprintf("%s pid=%d kind=%s via=%s idle=%.1fh", status, r.Pid, r.Kind, r.Via, r.IdleH))
		if !silent {
			fmt.Printf("  PID %d (%s): %s\n", r.Pid, r.Kind, status)
		}
	}
}

func logLine(s string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), s)
}

func printTable(rows []row, hours float64) {
	scopes := "zed"
	if flagAll {
		scopes = "ALL"
	} else {
		var ss []string
		for k := range flagScopes {
			ss = append(ss, k)
		}
		if len(ss) > 0 {
			sort.Strings(ss)
			scopes = strings.Join(ss, ",")
		}
	}
	fmt.Printf("\nAI agent processes (stale = idle > %.1fh, scope: %s):\n", hours, scopes)
	if len(rows) == 0 {
		fmt.Println("  none found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tKIND\tVIA\tSTARTED\tAGE(h)\tIDLE(h)\tCPU(s)\tWS(MB)\tFLAG")
	for _, r := range rows {
		flagStr := ""
		if r.Stale {
			if scopeHit(r.Via) {
				flagStr = "STALE"
			} else {
				flagStr = "idle(out-of-scope)"
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.1f\t%.1f\t%d\t%d\t%s\n",
			r.Pid, r.Kind, r.Via, r.Start.Format("01-02 15:04"), r.AgeH, r.IdleH, r.CpuS, r.WsMB, flagStr)
	}
	w.Flush()
}

func confirm(q string) bool {
	fmt.Printf("%s [y/N]: ", q)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func waitEnter(listOnly bool) {
	if listOnly {
		return
	}
	fmt.Print("\nPress Enter to exit...")
	r := bufio.NewReader(os.Stdin)
	_, _ = r.ReadString('\n')
}

func hoursSince(t, now int64) float64 {
	if t <= 0 {
		return 0
	}
	return float64(now-t) / 3600
}

func doInstall() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("cannot find own path:", err)
		return
	}
	tr := fmt.Sprintf(`"%s" -sample`, exe)
	out, err := exec.Command("schtasks", "/Create", "/F", "/SC", "MINUTE", "/MO", "5",
		"/TN", "AgentReaperSample", "/TR", tr).CombinedOutput()
	if err != nil {
		fmt.Println("install failed:", strings.TrimSpace(string(out)))
		return
	}
	fmt.Println("Task 'AgentReaperSample' registered: samples every 5 minutes.")
	fmt.Println("Then just double-click agent-reaper.exe anytime to review & kill stale agents.")
}

func doUninstall() {
	out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", "AgentReaperSample").CombinedOutput()
	if err != nil {
		fmt.Println("uninstall failed:", strings.TrimSpace(string(out)))
		return
	}
	fmt.Println("Task 'AgentReaperSample' removed.")
}

// values plumbed in from flags after flag.Parse()
var flagHours = 6.0
var flagAll = false
var flagScopes = map[string]bool{"zed": true}

func scopeHit(via string) bool {
	if flagAll {
		return true
	}
	return flagScopes[via]
}

func threshold() *float64 { return &flagHours }
