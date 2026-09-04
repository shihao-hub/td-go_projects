// Package backend 装配 aiquickd 的全部协议方法：
// hello / ping / config.* / presets.* / ask.stream / ask.cancel。
// 业务逻辑（提示词组装、LLM 调用、取消）全部在这里，server 与 store 只提供机制。
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"aiquick/internal/api"
	"aiquick/internal/llm"
	"aiquick/internal/protocol"
	"aiquick/internal/server"
	"aiquick/internal/store"
)

// Backend 后端服务实例。
type Backend struct {
	// Version hello 应答中的版本号，main 里通过 -ldflags 注入，默认 dev。
	Version string

	store *store.Store

	activeMu sync.Mutex
	active   map[int64]context.CancelFunc // rid -> 取消函数（ask.stream 在途请求）
}

// New 创建后端。
func New(st *store.Store) *Backend {
	return &Backend{Version: "dev", store: st, active: make(map[int64]context.CancelFunc)}
}

// Router 注册全部方法并返回路由表。
func (b *Backend) Router() *server.Router {
	r := server.NewRouter()
	r.Handle(protocol.MethodHello, b.handleHello)
	r.Handle(protocol.MethodPing, b.handlePing)
	r.Handle("config.get", b.handleConfigGet)
	r.Handle("config.set", b.handleConfigSet)
	r.Handle("presets.list", b.handlePresetsList)
	r.Handle("presets.save", b.handlePresetsSave)
	r.Handle("presets.delete", b.handlePresetsDelete)
	r.Handle("ask.stream", b.handleAskStream)
	r.Handle("ask.cancel", b.handleAskCancel)
	return r
}

func (b *Backend) handleHello(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	return api.HelloResult{Proto: protocol.Version, Name: "aiquickd", Version: b.Version}, nil
}

func (b *Backend) handlePing(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	return nil, nil
}

func (b *Backend) handleConfigGet(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	return b.store.Config(), nil
}

func (b *Backend) handleConfigSet(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	var cfg api.Config
	if len(params) > 0 {
		if err := json.Unmarshal(params, &cfg); err != nil {
			return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("参数格式错误: %w", err))
		}
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.BaseURL == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("BaseURL 不能为空"))
	}
	if u, err := url.Parse(cfg.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("BaseURL 不是合法的 http(s) 地址: %s", cfg.BaseURL))
	}
	if cfg.Model == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("模型名不能为空"))
	}
	if err := b.store.SetConfig(cfg); err != nil {
		return nil, err
	}
	return b.store.Config(), nil
}

func (b *Backend) handlePresetsList(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	return b.store.Presets(), nil
}

func (b *Backend) handlePresetsSave(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	var p api.Preset
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("参数格式错误: %w", err))
		}
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.System = strings.TrimSpace(p.System)
	p.UserTemplate = strings.TrimSpace(p.UserTemplate)
	if p.Name == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("预设名称不能为空"))
	}
	if p.System == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("预设指令(System)不能为空"))
	}
	if strings.ContainsRune(p.Name, '\n') {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("预设名称不能包含换行"))
	}
	return b.store.SavePreset(p)
}

func (b *Backend) handlePresetsDelete(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	var q api.PresetIDParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &q); err != nil {
			return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("参数格式错误: %w", err))
		}
	}
	if err := b.store.DeletePreset(q.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, server.Coded(protocol.CodeNotFound, err)
		}
		return nil, err
	}
	return nil, nil
}

// buildMessages 由预设与用户输入组装 LLM 消息。
// UserTemplate 含 {{input}} 时做替换；不含占位符时模板后换行拼接输入；无模板时输入即 user 消息。
func buildMessages(preset api.Preset, params api.AskParams) []llm.Message {
	system := params.System
	if system == "" {
		system = preset.System
	}
	user := params.Input
	if preset.UserTemplate != "" {
		if strings.Contains(preset.UserTemplate, "{{input}}") {
			user = strings.ReplaceAll(preset.UserTemplate, "{{input}}", params.Input)
		} else {
			user = preset.UserTemplate + "\n" + params.Input
		}
	}
	msgs := make([]llm.Message, 0, 2)
	if system != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: system})
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: user})
	return msgs
}

func (b *Backend) handleAskStream(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	var p api.AskParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("参数格式错误: %w", err))
		}
	}
	p.Input = strings.TrimSpace(p.Input)
	if p.Input == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("输入不能为空"))
	}
	var preset api.Preset
	if p.System == "" {
		if p.PresetID == "" {
			return nil, server.Coded(protocol.CodeBadRequest, errors.New("未选择预设"))
		}
		found := false
		for _, ps := range b.store.Presets() {
			if ps.ID == p.PresetID {
				preset, found = ps, true
				break
			}
		}
		if !found {
			return nil, server.Coded(protocol.CodeNotFound, fmt.Errorf("预设不存在: %s", p.PresetID))
		}
	}

	cfg := b.store.Config()
	if cfg.APIKey == "" {
		return nil, server.Coded(protocol.CodeBadRequest, errors.New("尚未配置 API Key，请先打开设置填写"))
	}

	// 注册取消句柄：ask.cancel 按 rid 取消本请求
	rctx, cancel := context.WithCancel(ctx)
	b.activeMu.Lock()
	b.active[rid] = cancel
	b.activeMu.Unlock()
	defer func() {
		cancel()
		b.activeMu.Lock()
		delete(b.active, rid)
		b.activeMu.Unlock()
	}()

	client := &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}
	msgs := buildMessages(preset, p)
	full, err := client.ChatStream(rctx, msgs, func(delta string) {
		emit(api.EventChunk, api.ChunkData{Text: delta})
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, server.Coded(protocol.CodeCancelled, errors.New("已取消"))
		}
		return nil, server.Coded(protocol.CodeUpstream, err)
	}
	return api.AskResult{Text: full}, nil
}

func (b *Backend) handleAskCancel(ctx context.Context, rid int64, params json.RawMessage, emit server.Emit) (any, error) {
	var q api.CancelParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &q); err != nil {
			return nil, server.Coded(protocol.CodeBadRequest, fmt.Errorf("参数格式错误: %w", err))
		}
	}
	cancel, ok := b.lookupCancel(q.RID)
	if ok {
		cancel()
	}
	return map[string]bool{"cancelled": ok}, nil
}

// lookupCancel 查找在途请求的取消句柄。
// 请求分发是并发的，ask.cancel 可能赶在 ask.stream 注册取消句柄之前到达，
// 这里短暂轮询以吸收竞态（ask.stream 尚未出现即视为确实不在途）。
func (b *Backend) lookupCancel(rid int64) (context.CancelFunc, bool) {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		b.activeMu.Lock()
		cancel, ok := b.active[rid]
		b.activeMu.Unlock()
		if ok {
			return cancel, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
