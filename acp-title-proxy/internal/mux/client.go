package mux

import (
	"bufio"
	"bytes"
	"net"
	"sync"
)

// Client 表示一个 serve 连接（对应 Zed 的一个 agent panel）。
type Client struct {
	id     string
	conn   net.Conn
	reader *bufio.Reader
	d      *Daemon

	writeMu sync.Mutex
}

func (c *Client) send(line []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return
	}
	_, _ = c.conn.Write(append(line, '\n'))
}

func (c *Client) readLoop() {
	defer func() {
		_ = c.conn.Close()
		c.d.removeClient(c)
	}()

	for {
		line, err := c.reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			c.d.fromClient(c, line)
		}
		if err != nil {
			return
		}
	}
}
