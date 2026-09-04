// 在 file-sync-native/ 下新建 cmd/cli/main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"file-sync-native/config"
	"file-sync-native/engine"
	"file-sync-native/models"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		panic(err) // sh-note: 学习阶段直接 panic
	}
	cache := engine.LoadHashCache("cache.gob")
	task := cfg.Tasks[0] // 第一个任务
	fmt.Println("task: ", task)
	bufio.NewReader(os.Stdin).ReadString('\n')
	
	tracker := engine.NewTracker(task.ID)
	tracker.SetNotify(func(p models.SyncProgress) {
		fmt.Printf("\r进度: %.1f%% | %s", p.Percentage, p.CurrentPath)
	})

	res, _ := engine.Run(context.Background(), task, cache, tracker, engine.Options{
		OnPendingDeletes: func(deletes []string) bool {
			fmt.Println("\n待删除文件:", len(deletes), "个")
			fmt.Print("确认删除? (y/n): ")
			var ans string
			fmt.Scan(&ans)
			return ans == "y"
		},
	})

	fmt.Printf("\n完成: %d 复制, %d 删除\n", res.Copied, res.Deleted)
}
