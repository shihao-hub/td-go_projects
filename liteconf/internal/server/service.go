// Package server 的 Service 聚合：把存储、广播与外部编辑检测组装成
// GUI 与 CLI serve 共用的同一套 HTTP 数据面。
package server

import (
	"context"
	"net/http"
	"time"
)

// Service 是一个可直接对外提供服务的 liteconf 实例。
type Service struct {
	Store       *Store
	Broadcaster *Broadcaster
	poller      *Poller
}

// NewService 创建服务：加载配置根目录、接入广播、准备外部编辑检测。
func NewService(root string, pollInterval time.Duration) (*Service, error) {
	bc := NewBroadcaster()
	st, err := NewStore(root, bc.Notify)
	if err != nil {
		return nil, err
	}
	return &Service{
		Store:       st,
		Broadcaster: bc,
		poller:      NewPoller(st, pollInterval),
	}, nil
}

// RunPoller 周期检测绕过 API 的外部文件编辑，直到 ctx 取消。
func (s *Service) RunPoller(ctx context.Context) {
	s.poller.Run(ctx)
}

// Handler 返回 GUI 与 CLI serve 共用的 HTTP handler（WebUI + REST）。
func (s *Service) Handler() http.Handler {
	return NewMux(s.Store, s.Broadcaster)
}
