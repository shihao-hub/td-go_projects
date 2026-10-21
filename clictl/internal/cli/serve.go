package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"clictl/internal/api"
	"clictl/internal/appinfo"
	"clictl/internal/service"
)

// cmdServe 启动内置 WebUI 与 REST 服务（GUI 与 CLI 共用同一 Handler）。
// 这是 console 通道唯一会阻塞的常驻命令。
func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "HTTP 端口（0 = 系统动态探测可用端口）")
	open := fs.Bool("open", false, "启动后自动打开默认浏览器")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		return failBadArgs("用法: clictl serve [--port 0] [--open]")
	}

	dbPath := appinfo.DefaultDBPath()
	svc, err := service.OpenAt(dbPath)
	if err != nil {
		return failService(err, false)
	}
	defer svc.Close()

	server := api.NewServer(svc, api.Meta{
		Channel:   appinfo.Channel(),
		BuildMode: appinfo.BuildMode,
		Version:   appinfo.Version,
		DBPath:    dbPath,
	})
	if _, err := server.Start(*port, *open); err != nil {
		return failBadArgs("服务异常终止: " + err.Error())
	}
	return 0
}

func failBadArgs(message string) int {
	Fail("bad_args", message)
	return 1
}

// whereOutput 是 where 子命令的机器可读契约，供 Agent 自证通道与库位。
type whereOutput struct {
	Binary         string    `json:"binary"`
	Channel        string    `json:"channel"`
	BuildMode      string    `json:"build_mode"`
	Version        string    `json:"version"`
	DBPath         string    `json:"db_path"`
	DBExists       bool      `json:"db_exists"`
	Tools          int       `json:"tools"`
	Running        int       `json:"running"`
	RunningGUIs    whereGUIs `json:"running_guis"`
	Recommendation string    `json:"recommendation"`
}

type whereGUIs struct {
	Dev  bool `json:"dev"`
	Prod bool `json:"prod"`
}

// whereRecommendation 给出本二进制与运行中 GUI 的匹配结论。
func whereRecommendation(channel string, guis whereGUIs) string {
	running := map[string]bool{"dev": guis.Dev, "prod": guis.Prod}
	if running[channel] {
		return "OK：本 CLI 与运行中的 GUI 同通道，写入后窗口刷新即可见"
	}
	for _, other := range []string{"dev", "prod"} {
		if other != channel && running[other] {
			return fmt.Sprintf("注意：GUI 正运行在 %s 通道，要让窗口直接看到请改用 %s", other, channelExeHint(other))
		}
	}
	if channel == "prod" {
		return "当前无 GUI 运行；本 CLI 写入 prod 通道库，与用户日常双击的正式版一致"
	}
	return "当前无 GUI 运行；本 CLI 写入 dev 通道库（仅供开发调试；日常请用 clictl-cli.exe 写 prod）"
}

// channelExeHint 返回对应通道的 GUI 产物名（release 无后缀，dev 带 -dev）。
func channelExeHint(channel string) string {
	if channel == "dev" {
		return "clictl-gui-dev.exe"
	}
	return "clictl-gui.exe"
}

// cmdWhere 自省：通道、库位、工具数、两通道 GUI 运行状态。
func cmdWhere(args []string) int {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbOverride := fs.String("db", "", "覆盖数据库路径（仅调试用）")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	dbPath := appinfo.DefaultDBPath()
	if *dbOverride != "" {
		dbPath = *dbOverride
	}

	// 先 stat 再打开：where 是只读自省，不能因为好奇就在另一个通道凭空造库。
	dbExists := false
	tools, running := 0, 0
	if _, err := os.Stat(dbPath); err == nil {
		dbExists = true
		if svc, err := service.OpenAt(dbPath); err == nil {
			if list, err := svc.ListTools(""); err == nil {
				tools = len(list)
			}
			if list, err := svc.ListRunning(); err == nil {
				running = len(list)
			}
			_ = svc.Close()
		}
	}

	info := whereOutput{
		Binary:      filepath.Base(os.Args[0]),
		Channel:     appinfo.Channel(),
		BuildMode:   appinfo.BuildMode,
		Version:     appinfo.Version,
		DBPath:      dbPath,
		DBExists:    dbExists,
		Tools:       tools,
		Running:     running,
		RunningGUIs: whereGUIs{Dev: appinfo.GUIRunning(false), Prod: appinfo.GUIRunning(true)},
	}
	info.Recommendation = whereRecommendation(info.Channel, info.RunningGUIs)

	// 与其余管理命令一致：永远输出 JSON 包络。
	Emit(info)
	return 0
}
