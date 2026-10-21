package app

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"agyquota/internal/service"
	"agyquota/internal/webui"
)

// newWebHandler 构造 GUI 内嵌 WebUI：与 CLI serve 共用 internal/webui，
// 因此两条通道看到的是同一套页面与 API。
func newWebHandler() http.Handler {
	return webui.New(service.New(), logf).Handler()
}

// listenEmbedded 绑定回环地址；port=0 时由内核动态探测可用端口。
func listenEmbedded(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// startEmbeddedServer 在后台启动 WebUI 服务并返回实际访问地址。
func startEmbeddedServer(port int) (string, error) {
	listener, err := listenEmbedded(port)
	if err != nil {
		return "", fmt.Errorf("启动 WebUI 监听失败: %w", err)
	}
	url := "http://" + listener.Addr().String()
	logf("WebUI 已启动: %s", url)
	go func() {
		if err := http.Serve(listener, newWebHandler()); err != nil {
			logf("WebUI 服务退出: %v", err)
		}
	}()
	return url, nil
}

// RunHeadless 是桌面壳不可用（如 WebView2 运行时缺失）时的兜底：
// 只起 WebUI HTTP 服务，可选打开默认浏览器，阻塞至进程退出。
func RunHeadless(openBrowser bool) error {
	listener, err := listenEmbedded(0)
	if err != nil {
		return fmt.Errorf("启动 WebUI 监听失败: %w", err)
	}
	url := "http://" + listener.Addr().String()
	logf("桌面壳不可用，已降级为无窗口 WebUI: %s", url)
	if openBrowser {
		go func() {
			if err := openURL(url); err != nil {
				logf("打开浏览器失败: %v", err)
			}
		}()
	}
	return http.Serve(listener, newWebHandler())
}
