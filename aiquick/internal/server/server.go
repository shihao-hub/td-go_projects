// Package server 实现 aiquickd 的行式 JSON 协议服务端：
// 阻塞读 io.Reader 的每一行请求，路由到 Handler，应答与事件写回 io.Writer。
// 基于纯 io 抽象，可脱离真实进程做表驱动测试。
package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"aiquick/internal/protocol"
)

// Emit 在 handler 执行过程中向后端连接推送事件；事件自动携带请求 rid。
type Emit func(topic string, data any)

// Handler 处理一次请求。rid 为请求 id（用于流式关联/取消）；
// 返回 (result, nil) 序列化为 Response.Result；返回 error 序列化为 Response.Error。
type Handler func(ctx context.Context, rid int64, params json.RawMessage, emit Emit) (any, error)

// Router 方法路由表。
type Router struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRouter() *Router {
	return &Router{handlers: make(map[string]Handler)}
}

// Handle 注册方法。
func (r *Router) Handle(method string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = h
}

func (r *Router) lookup(method string) Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[method]
}

// maxLineLen 单行消息上限（大结果保护）。
const maxLineLen = 8 << 20

// Serve 读循环：逐行解码 Request 并分发。handler 在独立 goroutine 执行，
// 长任务（ask.stream）不会阻塞后续请求（ask.cancel 需要这一点）。
// 内置 shutdown 方法：同步写出应答后直接返回 nil。
// 返回条件：读到 EOF / 读错误 / 收到 shutdown / ctx 取消后读侧结束。
func Serve(ctx context.Context, r io.Reader, w io.Writer, router *Router) error {
	var wmu sync.Mutex
	writeLine := func(v any) error {
		line, err := protocol.EncodeLine(v)
		if err != nil {
			return err
		}
		wmu.Lock()
		defer wmu.Unlock()
		_, err = w.Write(line)
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineLen)

	var wg sync.WaitGroup
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req protocol.Request
		if err := json.Unmarshal(line, &req); err != nil {
			slog.Warn("bad request line", "err", err)
			_ = writeLine(protocol.Response{
				ID:    0,
				OK:    false,
				Error: &protocol.Error{Code: protocol.CodeBadRequest, Message: err.Error()},
			})
			continue
		}
		if req.Method == protocol.MethodShutdown {
			// 同步应答后退出读循环；由调用方负责进程收尾。
			_ = writeLine(protocol.Response{ID: req.ID, OK: true})
			wg.Wait()
			return nil
		}
		wg.Add(1)
		go func(req protocol.Request) {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					slog.Error("handler panic", "method", req.Method, "panic", p)
					_ = writeLine(protocol.Response{
						ID:    req.ID,
						OK:    false,
						Error: &protocol.Error{Code: protocol.CodeInternal, Message: fmt.Sprintf("panic: %v", p)},
					})
				}
			}()
			resp := dispatch(ctx, router, req, writeLine)
			if err := writeLine(resp); err != nil {
				slog.Error("write response", "method", req.Method, "err", err)
			}
		}(req)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read loop: %w", err)
	}
	// EOF：等待在途 handler 结束（进程即将退出）
	wg.Wait()
	return nil
}

func dispatch(ctx context.Context, router *Router, req protocol.Request, writeLine func(any) error) protocol.Response {
	emit := func(topic string, data any) {
		payload, err := json.Marshal(data)
		if err != nil {
			slog.Error("emit marshal", "topic", topic, "err", err)
			return
		}
		if err := writeLine(protocol.Event{Event: topic, RID: req.ID, Data: payload}); err != nil {
			slog.Error("emit write", "topic", topic, "err", err)
		}
	}

	h := router.lookup(req.Method)
	if h == nil {
		return protocol.Response{
			ID:    req.ID,
			OK:    false,
			Error: &protocol.Error{Code: protocol.CodeNotFound, Message: "unknown method: " + req.Method},
		}
	}
	result, err := h(ctx, req.ID, req.Params, emit)
	if err != nil {
		return protocol.Response{
			ID:    req.ID,
			OK:    false,
			Error: toProtocolError(err),
		}
	}
	if result == nil {
		return protocol.Response{ID: req.ID, OK: true}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return protocol.Response{
			ID:    req.ID,
			OK:    false,
			Error: &protocol.Error{Code: protocol.CodeInternal, Message: "marshal result: " + err.Error()},
		}
	}
	return protocol.Response{ID: req.ID, OK: true, Result: raw}
}

type codedError struct {
	code    string
	wrapped error
}

// Coded 构造带协议错误码的错误，handler 返回它可控制 Response.Error.Code。
func Coded(code string, err error) error { return &codedError{code: code, wrapped: err} }

func (e *codedError) Error() string {
	if e.wrapped == nil {
		return e.code
	}
	return e.wrapped.Error()
}
func (e *codedError) Unwrap() error { return e.wrapped }

func toProtocolError(err error) *protocol.Error {
	var ce *codedError
	if errors.As(err, &ce) {
		msg := ce.Error()
		if msg == "" {
			msg = ce.code
		}
		return &protocol.Error{Code: ce.code, Message: msg}
	}
	return &protocol.Error{Code: protocol.CodeInternal, Message: err.Error()}
}
