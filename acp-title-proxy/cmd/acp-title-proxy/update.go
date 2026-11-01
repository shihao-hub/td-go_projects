package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"language_projects/go_projects/acp-title-proxy/internal/updater"
)

func runUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "仅检查是否有新版本，不执行下载")
	force := fs.Bool("force", false, "强制重新下载（即使本地已安装）")
	remote := fs.Bool("remote", false, "绕过本地 registry.json，直接从官方 CDN 获取最新版本信息")
	keep := fs.Int("keep", 2, "保留最近的旧版本数量（其余自动清理）")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	fmt.Println("[acp-title-proxy] Checking for Antigravity (AGY) updates...")

	check, err := updater.Check(!*remote)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Error checking update: %v\n", err)
		return 1
	}

	fmt.Printf("  - Current installed: %s\n", check.CurrentVersion)
	fmt.Printf("  - Registry latest:   %s\n", check.LatestVersion)
	fmt.Printf("  - Target directory:  %s\n", check.TargetDir)

	if check.AlreadyInstalled && !*force {
		fmt.Printf("[acp-title-proxy] AGY is already up to date (%s).\n", check.LatestVersion)
		return 0
	}

	if *checkOnly {
		if check.HasUpdate {
			fmt.Printf("[acp-title-proxy] New version %s is available!\n", check.LatestVersion)
		} else {
			fmt.Println("[acp-title-proxy] No updates available.")
		}
		return 0
	}

	fmt.Printf("[acp-title-proxy] Downloading AGY %s ...\n", check.LatestVersion)
	fmt.Printf("  Archive: %s\n", check.ArchiveURL)

	var lastPercent int = -1
	progress := func(downloaded, total int64) {
		if total > 0 {
			percent := int((downloaded * 100) / total)
			if percent != lastPercent && percent%10 == 0 {
				lastPercent = percent
				fmt.Printf("  Progress: %d%% (%d / %d MB)\n", percent, downloaded/(1024*1024), total/(1024*1024))
			}
		}
	}

	opts := updater.UpdateOptions{
		PreferLocal: !*remote,
		Force:       *force,
		KeepCount:   *keep,
		Progress:    progress,
		Logger:      log.Default(),
	}

	res, err := updater.Update(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Update failed: %v\n", err)
		return 1
	}

	if res.Updated {
		fmt.Printf("[acp-title-proxy] Successfully installed AGY %s to:\n  %s\n", res.LatestVersion, res.TargetDir)
		if len(res.Pruned) > 0 {
			fmt.Printf("  Cleaned up %d older versions: %v\n", len(res.Pruned), res.Pruned)
		}
		fmt.Println("[acp-title-proxy] The new version will be used on the next worker restart.")
	} else {
		fmt.Println("[acp-title-proxy] AGY is already up to date.")
	}

	return 0
}
