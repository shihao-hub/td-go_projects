package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shihao-hub/liteconf/internal/appenv"
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
	serverURL := fs.String("server", "", "liteconf server 地址（默认自动发现运行中的 GUI/serve，回退 "+mcp.DefaultServerURL+"）")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(opt.Stderr, "liteconf mcp: 参数解析失败")
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: 未知参数 %q（mcp 子命令只接受 -server）\n", fs.Arg(0))
		return 2
	}
	resolved := *serverURL
	if resolved == "" {
		// 动态端口下 GUI/serve 的实际地址不可预知，优先读取其写入的 runtime.json
		// （当前通道优先，未命中再尝试另一通道），最后回退默认地址。
		if rt, ok := appenv.DiscoverRuntime(); ok {
			resolved = rt.URL
		} else {
			resolved = mcp.DefaultServerURL
		}
	}
	if !strings.HasPrefix(resolved, "http://") && !strings.HasPrefix(resolved, "https://") {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: -server 必须是 http(s):// 开头的地址，当前为 %q\n", resolved)
		return 2
	}

	fmt.Fprintf(opt.Stderr, "liteconf mcp: stdio MCP server 启动，server=%s（协议消息走 stdin/stdout）\n", resolved)
	cfg := mcp.Config{ServerURL: resolved, Version: version.Version}
	if err := mcp.Run(context.Background(), cfg, &sdkmcp.StdioTransport{}); err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf mcp: 运行失败：%v\n", err)
		return 1
	}
	return 0
}
