package transport

import (
	"io"
	"net"
	"testing"
	"time"
	"thermalsim/pkg/printer"
)

func TestTCPBridgeStatusRouting(t *testing.T) {
	// Start TCP listener on loopback dynamic port
	tcpBridge, err := NewTCPBridge("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start TCPBridge: %v", err)
	}
	defer tcpBridge.Close()

	prof := &mockProfile{}
	reg := printer.NewRegisters(prof)
	head := printer.NewThermalHead(384)
	fsm := printer.NewFSM(prof, reg, head)
	ringBuf := NewRingBuffer(4096)

	bridge := NewBridge(nil, ringBuf, fsm)
	bridge.Start()
	defer bridge.Stop()

	// Launch accept goroutine
	acceptedChan := make(chan net.Conn, 1)
	go func() {
		conn, aErr := tcpBridge.AcceptConn()
		if aErr == nil {
			acceptedChan <- conn
		}
	}()

	// Dial from client POS simulator
	client, err := net.Dial("tcp", tcpBridge.Addr())
	if err != nil {
		t.Fatalf("failed to dial TCPBridge: %v", err)
	}
	defer client.Close()

	var serverConn net.Conn
	select {
	case serverConn = <-acceptedChan:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for TCP accept")
	}
	defer serverConn.Close()

	// Register serverConn to bridge
	bridge.AttachWriter(serverConn)
	defer bridge.DetachWriter(serverConn)

	// Pump ingress bytes into bridge
	go func() {
		buf := make([]byte, 256)
		for {
			n, rErr := serverConn.Read(buf)
			if n > 0 {
				bridge.PushBytes(buf[:n])
			}
			if rErr != nil {
				return
			}
		}
	}()

	// Client sends ESC v 0 (Query status)
	_, err = client.Write([]byte{0x1B, 'v', 0x00})
	if err != nil {
		t.Fatalf("failed to send ESC v to TCP bridge: %v", err)
	}

	// Read status response back from TCP socket
	_ = client.SetReadDeadline(time.Now().Add(1 * time.Second))
	resp := make([]byte, 1)
	n, err := io.ReadFull(client, resp)
	if err != nil || n != 1 {
		t.Fatalf("failed to receive status response over TCP: %v (n=%d)", err, n)
	}

	if (resp[0] & 0x01) == 0 {
		t.Fatalf("expected online bit 0 set in status response, got 0x%02X", resp[0])
	}
}

func TestBridgeBaudRatePacing(t *testing.T) {
	prof := &mockProfile{}
	reg := printer.NewRegisters(prof)
	head := printer.NewThermalHead(384)
	fsm := printer.NewFSM(prof, reg, head)
	ringBuf := NewRingBuffer(4096)

	bridge := NewBridge(nil, ringBuf, fsm)

	// 1. Unpaced (realTime = false): 200 bytes should be instant (< 10ms)
	bridge.SetRealTime(false, 9600)
	payload := make([]byte, 200)
	start := time.Now()
	bridge.PushBytes(payload)
	elapsed := time.Since(start)
	if elapsed > 10*time.Millisecond {
		t.Fatalf("expected unpaced push to be near instant, took %v", elapsed)
	}

	// 2. Real-Time paced at 9600 baud:
	// 10 bits/char = ~1.0416ms per byte.
	// 10 bytes = ~10.4ms.
	bridge.SetRealTime(true, 9600)
	start = time.Now()
	bridge.PushBytes(payload[:10])
	elapsedPaced := time.Since(start)
	if elapsedPaced < 8*time.Millisecond {
		t.Fatalf("expected paced push of 10 bytes at 9600 baud to take >= 8ms, took %v", elapsedPaced)
	}
}
