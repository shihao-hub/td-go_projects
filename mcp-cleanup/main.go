package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

//go:embed scripts/mcp-analyze.ps1
var analyzeScript []byte

//go:embed scripts/mcp-kill.ps1
var killScript []byte

var killListRe = regexp.MustCompile(`KILL LIST: (\d+) processes`)

func main() {
	threshold := flag.Float64("threshold", 2, "idle threshold in hours")
	dry := flag.Bool("dry", false, "analyze only, never kill")
	yes := flag.Bool("yes", false, "skip confirmation prompt")
	flag.Parse()

	scriptDir, err := extractScripts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-cleanup: extract scripts: %v\n", err)
		os.Exit(1)
	}

	outBuf := new(bytes.Buffer)
	code, err := runPS(scriptDir+"\\mcp-analyze.ps1", []string{
		"-ThresholdHours", fmt.Sprintf("%g", *threshold),
	}, io.MultiWriter(os.Stdout, outBuf))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-cleanup: run analyze: %v\n", err)
		os.Exit(1)
	}
	if code != 0 {
		fmt.Fprintf(os.Stderr, "mcp-cleanup: analyze failed with exit code %d\n", code)
		os.Exit(code)
	}

	killCount, parsed := parseKillCount(outBuf.Bytes())
	if parsed && killCount == 0 {
		fmt.Println("mcp-cleanup: nothing to clean.")
		os.Exit(0)
	}
	if *dry {
		fmt.Println("mcp-cleanup: dry run, kill plan saved but not executed.")
		os.Exit(0)
	}
	if !*yes {
		targetDesc := "unknown (parse failed, assuming non-empty)"
		if parsed {
			targetDesc = fmt.Sprintf("%d processes", killCount)
		}
		fmt.Printf("mcp-cleanup: about to kill %s.\n确认执行击杀? [y/N] ", targetDesc)
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("mcp-cleanup: aborted, nothing killed.")
			os.Exit(0)
		}
	}

	code, err = runPS(scriptDir+"\\mcp-kill.ps1", nil, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-cleanup: run kill: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func extractScripts() (string, error) {
	dir := os.Getenv("TEMP") + "\\mcp-cleanup"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dir+"\\mcp-analyze.ps1", analyzeScript, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(dir+"\\mcp-kill.ps1", killScript, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func runPS(script string, extraArgs []string, stdout io.Writer) (int, error) {
	args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}
	args = append(args, extraArgs...)
	cmd := exec.Command("powershell.exe", args...)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

func parseKillCount(raw []byte) (int, bool) {
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
	if err != nil {
		decoded = raw
	}
	m := killListRe.FindSubmatch(decoded)
	if m == nil {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(string(m[1]), "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}
