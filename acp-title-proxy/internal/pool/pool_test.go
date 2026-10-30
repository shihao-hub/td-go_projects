package pool

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestBufferedConn(t *testing.T) {
	// 创建一对内存管道
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// 模拟 bufio.Reader 中已缓冲的数据
	preBuffered := []byte("PRE_BUFFERED_DATA\n")
	mockReader := io.MultiReader(bytes.NewReader(preBuffered), c1)

	bconn := NewBufferedConn(c1, mockReader)

	// 在后台向 c2 写入后续数据
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = c2.Write([]byte("SUBSEQUENT_DATA\n"))
	}()

	buf := make([]byte, 1024)
	n, err := bconn.Read(buf)
	if err != nil {
		t.Fatalf("first read error: %v", err)
	}
	if string(buf[:n]) != "PRE_BUFFERED_DATA\n" {
		t.Fatalf("expected pre-buffered data, got: %s", string(buf[:n]))
	}

	n, err = bconn.Read(buf)
	if err != nil {
		t.Fatalf("second read error: %v", err)
	}
	if string(buf[:n]) != "SUBSEQUENT_DATA\n" {
		t.Fatalf("expected subsequent data, got: %s", string(buf[:n]))
	}
}
