package transport

import (
	"fmt"
	"io"
	"net"
	"sync"
)

// TCPBridge listens on a TCP port (e.g. :9100) and routes bytes to and from the printer engine.
type TCPBridge struct {
	listener net.Listener
	addr     string
	mu       sync.Mutex
	conn     net.Conn
	closed   bool
}

// NewTCPBridge starts a TCP listener on the given address (e.g. "127.0.0.1:9100").
func NewTCPBridge(addr string) (*TCPBridge, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return &TCPBridge{
		listener: ln,
		addr:     ln.Addr().String(),
	}, nil
}

// Addr returns the listening network address.
func (tb *TCPBridge) Addr() string {
	return tb.addr
}

// AcceptConn waits for an incoming connection, closing any existing active connection.
func (tb *TCPBridge) AcceptConn() (net.Conn, error) {
	conn, err := tb.listener.Accept()
	if err != nil {
		return nil, err
	}

	tb.mu.Lock()
	if tb.conn != nil {
		_ = tb.conn.Close()
	}
	tb.conn = conn
	tb.mu.Unlock()

	return conn, nil
}

// Close closes the TCP listener and active connection.
func (tb *TCPBridge) Close() error {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.closed = true
	if tb.conn != nil {
		_ = tb.conn.Close()
	}
	return tb.listener.Close()
}

// RouteConnection pumps bytes from conn into ring buffer and writes txOut to conn.
func RouteConnection(conn io.ReadWriter, rb *RingBuffer, txChan <-chan byte, stopChan <-chan struct{}) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Ingress goroutine: Read from conn -> Push to RingBuffer
	go func() {
		defer wg.Done()
		buf := make([]byte, 256)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				for i := 0; i < n; i++ {
					rb.Push(buf[i])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Egress goroutine: Read from txChan -> Write to conn
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			case b, ok := <-txChan:
				if !ok {
					return
				}
				_, _ = conn.Write([]byte{b})
			}
		}
	}()

	wg.Wait()
}
