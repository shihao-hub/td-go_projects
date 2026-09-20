package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shihao-hub/liteconf/internal/mcp"
	"github.com/shihao-hub/liteconf/internal/version"
)

// runMCP 启动 stdio MCP server：
// stdin/stdout 专用于协议（由 go-sdk StdioTransport 接管），
// 启动诊断写 stderr；本函数在连接关闭前不返回。
//
// 退出码约定：2 调用参数错误；1 server 构建或运行失败；
// 正常退出（客户端断开）返回 0。
func runMCP(args []string, opt *Options) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	serverURL := fs.String("server", mcp.DefaultServerURL, "liteconf server 地址")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(opt.Stderr, "liteconf mcp: 参数解析失败")
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: 未知参数 %q（mcp 子命令只接受 -server）\n", fs.Arg(0))
		return 2
	}
	if !strings.HasPrefix(*serverURL, "http://") && !strings.HasPrefix(*serverURL, "https://") {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: -server 必须是 http(s):// 开头的地址，当前为 %q\n", *serverURL)
		return 2
	}

	fmt.Fprintf(opt.Stderr, "liteconf mcp: stdio MCP server 启动，server=%s（协议消息走 stdin/stdout）\n", *serverURL)
	cfg := mcp.Config{ServerURL: *serverURL, Version: version.Version}
	if err := mcp.Run(context.Background(), cfg, &sdkmcp.StdioTransport{}); err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: 运行失败：%v\n", err)
		return 1
	}
	return 0
}
