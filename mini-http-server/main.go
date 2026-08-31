package main

import (
	"fmt"
	"net"
)

func main() {
	fmt.Println("hello")
	listener, err := net.Listen("tcp", "127.0.0.1:8787")
	if err != nil {
		panic("端口 8787 绑定失败: " + err.Error())
	}
	fmt.Println("mini-http-server 已启动: http://127.0.0.1:8787")

	sem := make(chan struct{}, 4) // 限并发 4
	server := &Server{}
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Print("连接建立失败：", err)
			continue
		}
		sem <- struct{}{} // 池满则阻塞，等价于任务排队
		go func() {
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					fmt.Println("连接处理时发生 panic:", r)
				}
			}()
			server.handleConnection(conn)
		}()
	}
}
