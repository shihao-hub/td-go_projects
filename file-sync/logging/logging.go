package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
)

const maxSize = 10 * 1024 * 1024

var logger atomic.Pointer[slog.Logger]

func init() {
	logger.Store(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

func Init(path string) error {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".file-sync", "file-sync.log")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return err
	}

	fileHandler := slog.NewTextHandler(&rotatingWriter{f: f, path: path}, nil)
	consoleHandler := slog.NewTextHandler(os.Stderr, nil)
	multi := slog.New(&multiHandler{handlers: []slog.Handler{fileHandler, consoleHandler}})
	logger.Store(multi)
	return nil
}

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: next}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: next}
}

type rotatingWriter struct {
	f    *os.File
	path string
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	if fi, err := w.f.Stat(); err == nil && fi.Size()+int64(len(p)) >= maxSize {
		w.f.Close()
		_ = os.Rename(w.path, w.path+".old")
		nf, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, err
		}
		w.f = nf
	}
	return w.f.Write(p)
}

func With(module string) *slog.Logger {
	return logger.Load().With(slog.String("module", module))
}

func Infof(module, format string, args ...any) {
	logger.Load().Info(fmt.Sprintf(format, args...), slog.String("module", module))
}

func Errorf(module, format string, args ...any) {
	logger.Load().Error(fmt.Sprintf(format, args...), slog.String("module", module))
}
