package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"mini-http-server/http"
)

type Server struct {
}

// handleConnection 处理连接，如：http 协议拆解、router 分流并响应等
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Println("handleConnection: ", conn)
	peer := conn.RemoteAddr().String()
	fmt.Println("peer: ", peer)
	var response string
	req, err := http.ParseRequest(bufio.NewReader(conn))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] 请求解析失败: %v\n", peer, err)
		response = http.BuildResponse(http.StatusBadRequest, "text/plain; charset=utf-8", "400 Bad Request\n")
	} else {
		response = s.route(&req)
	}
	if _, err := conn.Write([]byte(response)); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] 响应写入失败: %v\n", peer, err)
		return
	}
	statusLine := strings.SplitN(response, "\n", 2)[0] // 取第一行
	fmt.Printf("[%s] %s\n", peer, statusLine)
}

// route 路由：穷尽匹配
func (s *Server) route(req *http.Request) string {
	switch {
	case req.Method == "GET" && req.Path == "/":
		return http.BuildResponse(http.StatusOK, "text/html; charset=utf-8", s.indexPage())

	case req.Method == "GET" && req.Path == "/sleep":
		// 慢请求模拟：占用一个并发名额 5 秒（配合信号量 = 占一个工作线程）
		time.Sleep(5 * time.Second)
		return http.BuildResponse(http.StatusOK, "text/plain; charset=utf-8", "睡醒了（这个请求耗时 5 秒）\n")

	default: // 对应 (_, path) 通配兜底
		return http.BuildResponse(http.StatusNotFound, "text/html; charset=utf-8",
			fmt.Sprintf("<h1>404</h1><p>没有 %s 这个路由</p>", req.Path))
	}
}

func (s *Server) indexPage() string {
	return `<html><head><title>mini-http-server</title></head><body>
<h1>Hello, Go!</h1>
<p>手写的多线程 HTTP 服务器。</p>
<ul>
<li><a href="/">GET /</a>：本页</li>
<li><a href="/sleep">GET /sleep</a>：耗时 5 秒的慢请求（试试慢请求期间刷新首页）</li>
</ul></body></html>`
}
