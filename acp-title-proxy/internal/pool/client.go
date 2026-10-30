package pool

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

var (
	ErrDaemonOutdated = errors.New("daemon outdated")
)

// GetSelfModTime returns the modification time (UnixNano) of the current executable.
func GetSelfModTime() int64 {
	exePath, err := os.Executable()
	if err != nil {
		return 0
	}
	info, err := os.Stat(exePath)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// BufferedConn wraps net.Conn with an io.Reader to ensure any data
// buffered by bufio.Reader during handshake is not lost.
type BufferedConn struct {
	net.Conn
	r io.Reader
}

func NewBufferedConn(c net.Conn, r io.Reader) *BufferedConn {
	return &BufferedConn{
		Conn: c,
		r:    r,
	}
}

func (bc *BufferedConn) Read(b []byte) (int, error) {
	return bc.r.Read(b)
}

func (bc *BufferedConn) CloseWrite() error {
	if tc, ok := bc.Conn.(*net.TCPConn); ok {
		return tc.CloseWrite()
	}
	return nil
}

// TryAcquire attempts to connect to the daemon and acquire a pre-warmed worker stream.
func TryAcquire(port int, timeout time.Duration) (net.Conn, error) {
	if port <= 0 {
		port = DefaultPort
	}
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable on %s: %w", addr, err)
	}

	// Set deadline for handshake
	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))

	req := PoolRequest{
		Cmd:        "acquire",
		Agent:      "agy",
		ExeModTime: GetSelfModTime(),
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	reqData = append(reqData, '\n')
	if _, err := conn.Write(reqData); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to send acquire request: %w", err)
	}

	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read response from daemon: %w", err)
	}

	var resp PoolResponse
	if err := json.Unmarshal(respLine, &resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("invalid response from daemon: %w", err)
	}

	if resp.Status == "outdated" {
		_ = conn.Close()
		return nil, ErrDaemonOutdated
	}

	if resp.Status != "ok" {
		_ = conn.Close()
		return nil, fmt.Errorf("daemon could not assign worker: %s", resp.Msg)
	}

	// Reset deadline for normal bidirectional streaming
	_ = conn.SetDeadline(time.Time{})
	return NewBufferedConn(conn, reader), nil
}

// StopDaemon requests the daemon to gracefully stop and clean up all workers.
func StopDaemon(port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		return fmt.Errorf("daemon is not running (%w)", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	req := PoolRequest{Cmd: "stop"}
	reqData, _ := json.Marshal(req)
	reqData = append(reqData, '\n')
	if _, err := conn.Write(reqData); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return err
	}

	var resp PoolResponse
	if err := json.Unmarshal(line, &resp); err == nil && resp.Status == "ok" {
		return nil
	}
	return nil
}

// CheckStatus queries the pool status from daemon.
func CheckStatus(port int) (*PoolResponse, error) {
	if port <= 0 {
		port = DefaultPort
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
	req := PoolRequest{Cmd: "status"}
	reqData, _ := json.Marshal(req)
	reqData = append(reqData, '\n')
	_, _ = conn.Write(reqData)

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var resp PoolResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
