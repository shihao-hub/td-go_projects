package http

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrEmptyRequest = errors.New("空请求")

type MalformedRequestLineError struct{ Line string }

func (e *MalformedRequestLineError) Error() string { // 对应 impl Display
	return fmt.Sprintf("请求行格式非法: %s", e.Line)
}

type Request struct {
	Method  string
	Path    string
	Version string
	Headers map[string]string
}

// ParseRequest 解析 http 协议
func ParseRequest(reader *bufio.Reader) (Request, error) {
	requestLine, err := reader.ReadString('\n') // 读取第一行
	if err != nil && !errors.Is(err, io.EOF) {  // ? 运算符 → 手写检查 + %w 包装
		return Request{}, fmt.Errorf("读取请求失败: %w", err)
	}
	if requestLine == "" || strings.TrimSpace(requestLine) == "" {
		return Request{}, ErrEmptyRequest
	}

	// sh-note: http 请求行格式固定是 METHOD PATH VERSION
	parts := strings.SplitN(strings.TrimRight(requestLine, "\r\n"), " ", 3)
	if len(parts) != 3 { // 对应三个 Some 的 match，查 len 即可
		return Request{}, &MalformedRequestLineError{Line: strings.TrimSpace(requestLine)}
	}
	method, path, version := parts[0], parts[1], parts[2]

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return Request{}, fmt.Errorf("读取请求失败: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // 空行 = 头部结束
		}
		if k, v, ok := strings.Cut(line, ":"); ok { // 对应 split_once(':')
			headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		if err == io.EOF {
			break // 对应 read_line 返回 Ok(0) 跳出循环
		}
	}

	return Request{Method: method, Path: path, Version: version, Headers: headers}, nil
}

type Status int

const (
	StatusOK         Status = 200 // 直接把状态码当常量值，省掉 code() 方法
	StatusNotFound   Status = 404
	StatusBadRequest Status = 400
)

func (s Status) reason() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusNotFound:
		return "Not Found"
	default: // Rust match 穷尽匹配会编译期报错，Go switch 必须自己兜底
		return "Bad Request"
	}
}

func (s Status) code() int{
	return int(s)
}

func BuildResponse(status Status, contentType string, body string) string {
	return fmt.Sprintf(
		"HTTP/1.1 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status.code(), status.reason(), contentType, len(body), body,
	)
}
