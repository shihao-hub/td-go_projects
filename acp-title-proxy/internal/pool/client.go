package pool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

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

	req := PoolRequest{Cmd: "acquire", Agent: "agy"}
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

	if resp.Status != "ok" {
		_ = conn.Close()
		return nil, fmt.Errorf("daemon could not assign worker: %s", resp.Msg)
	}

	// Reset deadline for normal bidirectional streaming
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
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
