package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	lock "instancelock"
)

const (
	exitOK    = 0
	exitUsage = 2
	exitHeld  = 3
)

const usageText = `instancelock - 通用单实例检测工具（基于文件锁，key 相互隔离）

用法:
  instancelock try  --key KEY [--wait 5s]              快照检查: 0=空闲 3=已有实例（查完即走，不持锁）
  instancelock hold --key KEY [--wait 5s] [--ppid N]   持锁模式: 打印 "LOCKED <pid>" 后常驻阻塞，
                                                       由本进程替宿主持有锁; 3=已有实例
  instancelock list                                    列出所有 key 的当前持有状态

退出码:
  0  成功（try=当前空闲 / hold=锁已正常释放）
  3  key 已被其他实例持有
  2  参数或系统错误

宿主项目接入（hold 模式）:
  1. 启动时 spawn:  instancelock hold --key my-app --ppid <宿主PID>
     （或不给 --ppid，保持子进程 stdin 管道不断开，宿主死后管道 EOF 自动释放）
  2. 读到子进程 stdout 的 "LOCKED <pid>" 行 -> 继续启动；读到退出码 3 -> 宿主自行退出
  3. 宿主无论正常退出、崩溃还是被 kill，锁均由操作系统兜底释放，不会死锁

锁文件: <用户缓存目录>/instancelock/<key前缀>-<hash12>.lock（Windows 为 %LOCALAPPDATA%）
key 建议带命名空间，如 com.company.appname
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}
	var code int
	switch os.Args[1] {
	case "try":
		code = cmdTry(os.Args[2:])
	case "hold":
		code = cmdHold(os.Args[2:])
	case "list":
		code = cmdList(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n", os.Args[1])
		usage()
		os.Exit(exitUsage)
	}
	os.Exit(code)
}

func reportHolder(key string) {
	info, err := lock.ReadInfo(lock.PathOf(key))
	if err != nil {
		fmt.Fprintf(os.Stderr, "key=%s 已被其他进程持有\n", key)
		return
	}
	fmt.Fprintf(os.Stderr, "已有实例: key=%s pid=%d host=%s started=%s\n",
		info.Key, info.PID, info.Host, info.Started.Format("2006-01-02 15:04:05"))
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	_ = fs.Parse(args)
	entries, err := lock.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return exitUsage
	}
	if len(entries) == 0 {
		fmt.Println("(无锁文件)")
		return exitOK
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tKEY\tPID\tHOST\tSTARTED\tFILE")
	for _, e := range entries {
		if e.Held {
			fmt.Fprintf(w, "HELD\t%s\t%d\t%s\t%s\t%s\n",
				e.Info.Key, e.Info.PID, e.Info.Host,
				e.Info.Started.Format("2006-01-02 15:04:05"), filepath.Base(e.Path))
			continue
		}
		key := e.Info.Key
		if key == "" {
			key = "-"
		}
		fmt.Fprintf(w, "free\t%s\t-\t-\t-\t%s\n", key, filepath.Base(e.Path))
	}
	_ = w.Flush()
	return exitOK
}

func cmdTry(args []string) int {
	fs := flag.NewFlagSet("try", flag.ExitOnError)
	key := fs.String("key", "", "唯一标识本项目的 key（必填）")
	wait := fs.Duration("wait", 0, "等待已有实例释放的时长，如 5s（默认 0=不等待）")
	_ = fs.Parse(args)
	if *key == "" {
		fmt.Fprintln(os.Stderr, "try: 缺少 --key")
		return exitUsage
	}
	lk, err := lock.TryWait(*key, *wait)
	if err != nil {
		if errors.Is(err, lock.ErrHeld) {
			fmt.Println("LOCKED")
			reportHolder(*key)
			return exitHeld
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		return exitUsage
	}
	_ = lk.Close()
	fmt.Println("FREE")
	return exitOK
}

func cmdHold(args []string) int {
	fs := flag.NewFlagSet("hold", flag.ExitOnError)
	key := fs.String("key", "", "唯一标识本项目的 key（必填）")
	wait := fs.Duration("wait", 0, "等待已有实例释放的时长，如 5s（默认 0=不等待）")
	ppid := fs.Int("ppid", 0, "宿主进程 PID：给定后改为轮询宿主存活（stdin EOF 不再触发退出）")
	_ = fs.Parse(args)
	if *key == "" {
		fmt.Fprintln(os.Stderr, "hold: 缺少 --key")
		return exitUsage
	}
	lk, err := lock.TryWait(*key, *wait)
	if err != nil {
		if errors.Is(err, lock.ErrHeld) {
			fmt.Println("LOCKED")
			reportHolder(*key)
			return exitHeld
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		return exitUsage
	}
	defer lk.Close()
	if err := lk.WriteInfo(*key); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 写入持有者信息失败:", err)
	}
	fmt.Printf("LOCKED %d\n", os.Getpid())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	if *ppid > 0 {
		for lock.ProcessAlive(*ppid) {
			select {
			case <-sig:
				return exitOK
			case <-time.After(time.Second):
			}
		}
		return exitOK
	}
	stdinDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinDone)
	}()
	select {
	case <-sig:
	case <-stdinDone:
	}
	return exitOK
}
