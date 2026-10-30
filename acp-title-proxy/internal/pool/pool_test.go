package pool

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestBufferedConn(t *testing.T) {
	// Create an in-memory pipe pair
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Simulate buffered data from bufio.Reader
	preBuffered := []byte("PRE_BUFFERED_DATA\n")
	mockReader := io.MultiReader(bytes.NewReader(preBuffered), c1)

	bconn := NewBufferedConn(c1, mockReader)

	// In background, write subsequent data to c2
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
