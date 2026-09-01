package main

import (
	"encoding/json"
	"log"
	"time"
)

// captureOnce 采集一次当前目录状态；与上一条记录相同则跳过入库。
func captureOnce(st *store, last *string) {
	folders, src, err := currentFolders()
	if err != nil {
		log.Printf("读取 Sublime 会话失败: %v", err)
		return
	}
	j, err := json.Marshal(folders)
	if err != nil {
		return
	}
	js := string(j)
	if js == *last {
		return
	}
	if err := st.insertSnapshot(folders); err != nil {
		log.Printf("写入记录失败: %v", err)
		return
	}
	*last = js
	log.Printf("已记录 %d 个目录 (%s)", len(folders), src)
}

func pruneOnce(st *store, retainDays int) {
	n, err := st.prune(retainDays)
	if err != nil {
		log.Printf("清理过期记录失败: %v", err)
		return
	}
	if n > 0 {
		log.Printf("已清理 %d 条过期记录", n)
	}
}

// captureLoop 先立即采集一次，再按 interval 周期采集；每 24h 清理一次过期记录。
func captureLoop(st *store, interval time.Duration, retainDays int) {
	last := st.lastSnapshotFolders()
	captureOnce(st, &last)
	pruneOnce(st, retainDays)
	lastPrune := time.Now()

	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		captureOnce(st, &last)
		if time.Since(lastPrune) >= 24*time.Hour {
			lastPrune = time.Now()
			pruneOnce(st, retainDays)
		}
	}
}
