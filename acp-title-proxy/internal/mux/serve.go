package mux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
	"language_projects/go_projects/acp-title-proxy/internal/procguard"
)

// RunServe 是 Zed 每个 agent panel 启动的轻量入口：
// 优先连常驻 daemon；不可用时尝试拉起 daemon；仍失败则回退为直连后端。
func RunServe(cfg *config.Config, logger *log.Logger) error {
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Share.Port)

	conn, r, err := tryAttach(addr)
	if err == nil {
		logger.Printf("attached to share daemon at %s", addr)
		return splice(conn, r)
	}

	if !cfg.Share.FallbackDirect {
		return fmt.Errorf("share daemon unavailable: %w", err)
	}

	_ = LaunchDaemon()
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		conn, r, err = tryAttach(addr)
		if err == nil {
			logger.Printf("attached to share daemon at %s (after launch)", addr)
			return splice(conn, r)
		}
	}

	logger.Printf("share daemon unavailable (%v); falling back to direct backend", err)
	return runDirect(cfg, logger)
}

func tryAttach(addr string) (net.Conn, *bufio.Reader, error) {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return nil, nil, err
	}

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(`{"cmd":"attach","proto":1}` + "\n")); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	r := bufio.NewReaderSize(conn, 1<<20)
	line, err := r.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	var resp struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if resp.Status != "ok" {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("attach rejected: %s", resp.Msg)
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, r, nil
}

func splice(conn net.Conn, r *bufio.Reader) error {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		_ = conn.Close()
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(os.Stdout, r)
		_ = conn.Close()
		done <- struct{}{}
	}()

	<-done
	return nil
}

func runDirect(cfg *config.Config, logger *log.Logger) error {
	exe, _, err := finder.FindAGY(cfg.Share.Executable)
	if err != nil {
		return err
	}
	logger.Printf("direct backend: %s", exe)

	cmd := exec.Command(exe)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	// 直连后端绑定 Job Object：客户端退出（甚至被强杀）时，内核连带终止后端与其 harness。
	if guard, gerr := procguard.Attach(cmd.Process.Pid); gerr != nil {
		logger.Printf("job object attach failed (direct backend pid %d): %v", cmd.Process.Pid, gerr)
	} else {
		defer guard.Close()
	}
	return cmd.Wait()
}
