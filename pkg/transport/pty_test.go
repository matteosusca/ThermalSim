package transport

import (
	"os"
	"testing"
	"time"
	"thermalsim/pkg/printer"
)

type mockProfile struct{}

func (m *mockProfile) Name() string                   { return "QR203" }
func (m *mockProfile) FirmwareVersion() printer.FirmwareRevision { return printer.FirmwareV269 }
func (m *mockProfile) DotsPerLine() int              { return 384 }
func (m *mockProfile) BytesPerLine() int             { return 48 }
func (m *mockProfile) DefaultBaudRate() int          { return 19200 }
func (m *mockProfile) ReceiveBufferSize() int        { return 4096 }
func (m *mockProfile) DefaultLineSpacing() uint8     { return 30 }
func (m *mockProfile) MaxHeatingDots() uint8         { return 7 }
func (m *mockProfile) DefaultHeatingTime() uint8     { return 80 }
func (m *mockProfile) DefaultHeatingInterval() uint8 { return 2 }
func (m *mockProfile) RenderChar(code byte, font printer.FontType, codePage int, intlSet int) []byte {
	return make([]byte, 12*24)
}
func (m *mockProfile) RenderKanji(c1, c2 byte) []byte   { return make([]byte, 24*24) }
func (m *mockProfile) SupportsQRCode() bool             { return true }
func (m *mockProfile) SupportsBarcode(system int) bool { return true }
func (m *mockProfile) GenerateBarcode(mId int, data []byte, reg *printer.Registers) *printer.BarcodeResult {
	return nil
}
func (m *mockProfile) ProcessQRCommand(fn byte, payload []byte, reg *printer.Registers) [][]uint8 {
	return nil
}
func (m *mockProfile) GenerateSelfTestPage(reg *printer.Registers) [][]uint8 {
	return nil
}

func TestPTYLoopbackAndStatus(t *testing.T) {
	symlink := "/tmp/qr203-test-tty"
	pty, err := OpenPTY(symlink)
	if err != nil {
		t.Fatalf("failed to open PTY: %v", err)
	}
	defer pty.Close()

	prof := &mockProfile{}
	reg := printer.NewRegisters(prof)
	head := printer.NewThermalHead(384)
	fsm := printer.NewFSM(prof, reg, head)
	ringBuf := NewRingBuffer(4096)

	bridge := NewBridge(pty, ringBuf, fsm)
	bridge.Start()
	defer bridge.Stop()

	// Open slave device from simulated host side
	slave, err := os.OpenFile(symlink, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open slave device %s: %v", symlink, err)
	}
	defer slave.Close()

	// 1. Host writes "HELLO\n"
	_, err = slave.Write([]byte("HELLO\n"))
	if err != nil {
		t.Fatalf("failed to write to slave: %v", err)
	}

	// Give processor time to drain
	time.Sleep(50 * time.Millisecond)
	if head.TotalRows() == 0 {
		t.Fatal("expected head to have burned rows after 'HELLO\\n'")
	}

	// 2. Host sends ESC v 0 (query status)
	_, err = slave.Write([]byte{0x1B, 'v', 0x00})
	if err != nil {
		t.Fatalf("failed to write ESC v to slave: %v", err)
	}

	// Read status back from slave
	statusBuf := make([]byte, 1)
	_ = slave.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, err := slave.Read(statusBuf)
	if err != nil || n != 1 {
		t.Fatalf("failed to read status response back from slave: %v (n=%d)", err, n)
	}

	if (statusBuf[0] & 0x01) == 0 {
		t.Fatalf("expected online bit set in status, got 0x%02X", statusBuf[0])
	}
}
