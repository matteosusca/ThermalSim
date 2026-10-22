package printer

import (
	"testing"
)

type mockProfile struct{}

func (m *mockProfile) Name() string                   { return "QR203" }
func (m *mockProfile) FirmwareVersion() FirmwareRevision { return FirmwareV269 }
func (m *mockProfile) DotsPerLine() int              { return 384 }
func (m *mockProfile) BytesPerLine() int             { return 48 }
func (m *mockProfile) DefaultBaudRate() int          { return 19200 }
func (m *mockProfile) ReceiveBufferSize() int        { return 4096 }
func (m *mockProfile) DefaultLineSpacing() uint8     { return 30 }
func (m *mockProfile) MaxHeatingDots() uint8         { return 7 }
func (m *mockProfile) DefaultHeatingTime() uint8     { return 80 }
func (m *mockProfile) DefaultHeatingInterval() uint8 { return 2 }
func (m *mockProfile) RenderChar(code byte, font FontType, codePage int, intlSet int) []byte {
	// Return a dummy 12x24 glyph with non-zero dots
	glyph := make([]byte, 12*24)
	glyph[0] = 1
	glyph[11] = 1
	return glyph
}
func (m *mockProfile) RenderKanji(c1, c2 byte) []byte   { return make([]byte, 24*24) }
func (m *mockProfile) GenerateBarcode(mId int, data []byte, reg *Registers) *BarcodeResult {
	rows := make([][]uint8, 50)
	for i := range rows {
		rows[i] = make([]uint8, 384)
	}
	return &BarcodeResult{Rows: rows, HRI: string(data)}
}

func (m *mockProfile) ProcessQRCommand(fn byte, payload []byte, reg *Registers) [][]uint8 {
	return nil
}

func (m *mockProfile) GenerateSelfTestPage(reg *Registers) [][]uint8 {
	return make([][]uint8, 300)
}

func (m *mockProfile) SupportsQRCode() bool             { return true }
func (m *mockProfile) SupportsBarcode(system int) bool { return true }

func TestFSMNormalTextAndLF(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// Send "HI" followed by LF
	fsm.Step('H')
	fsm.Step('I')
	if !fsm.lineBuf.hasContent {
		t.Fatal("expected lineBuf to have content after receiving 'H', 'I'")
	}
	if head.TotalRows() != 0 {
		t.Fatal("no rows should be burned before LF")
	}

	// Trigger LF
	fsm.Step(0x0A)
	if fsm.lineBuf.hasContent {
		t.Fatal("expected lineBuf to be cleared after LF")
	}
	// Height of line buffer (24) + feed (30) => rows burned
	if head.TotalRows() < 24 {
		t.Fatalf("expected head to have burned at least 24 rows, got %d", head.TotalRows())
	}
}

func TestFSMDC2BitmapAndLockupQuirk(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// DC2 * r n [data...]
	// Send DC2 * 2 4 (2 rows, 4 bytes per row = 8 total bytes)
	fsm.Step(0x12) // DC2
	fsm.Step('*')  // 0x2A
	fsm.Step(2)    // r = 2
	fsm.Step(4)    // n = 4

	if fsm.state != StateRawBitmap {
		t.Fatalf("expected state StateRawBitmap, got %v", fsm.state)
	}
	if fsm.bitmapRemaining != 8 {
		t.Fatalf("expected 8 bytes remaining, got %d", fsm.bitmapRemaining)
	}

	// Send only 4 bytes (1st row full, but 4 bytes still missing)
	fsm.Step(0xFF)
	fsm.Step(0xFF)
	fsm.Step(0xFF)
	fsm.Step(0xFF)

	if fsm.state != StateRawBitmap {
		t.Fatal("FSM must still be locked in StateRawBitmap waiting for 4 more bytes")
	}

	// HARDWARE QUIRK TEST: Host sends "ESC @" followed by "HELLO".
	// The printer MUST NOT reset or print text; it MUST consume those 4 bytes as bitmap bits!
	fsm.Step(0x1B) // 'ESC' swallowed as 5th bitmap byte
	fsm.Step('@')  // '@' swallowed as 6th bitmap byte
	fsm.Step('H')  // 'H' swallowed as 7th bitmap byte
	fsm.Step('E')  // 'E' swallowed as 8th bitmap byte (remaining hits 0)

	if fsm.state != StateNormal {
		t.Fatalf("FSM should return to StateNormal after exactly 8 bytes, got %v", fsm.state)
	}

	// The remaining bytes of "HELLO" ('L', 'L', 'O') will now be treated as normal text
	fsm.Step('L')
	fsm.Step('L')
	fsm.Step('O')
	if !fsm.lineBuf.hasContent {
		t.Fatal("expected 'LLO' to be in the line buffer as normal text")
	}
}

func TestFSMStatusCommands(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// 1. ESC v 0 status query
	fsm.Step(0x1B)
	fsm.Step('v')
	fsm.Step(0x00)

	select {
	case status := <-fsm.TxChannel():
		// Normal state: Online (bit 0 = 1), Paper adequate (bit 2 = 0)
		if (status & 0x01) == 0 {
			t.Fatalf("expected online bit set in status, got 0x%02X", status)
		}
		if (status & 0x04) != 0 {
			t.Fatalf("expected paper present, got 0x%02X", status)
		}
	default:
		t.Fatal("expected status byte to be transmitted on TX")
	}

	// 2. Set Paper Out and test status
	reg.PaperState = PaperOut
	fsm.Step(0x1B)
	fsm.Step('v')
	fsm.Step(0x00)

	select {
	case status := <-fsm.TxChannel():
		if (status & 0x04) == 0 {
			t.Fatalf("expected PaperOut bit (0x04) set in status, got 0x%02X", status)
		}
	default:
		t.Fatal("expected status byte on TX")
	}

	// 3. GS r quirk: when paper is out, GS r must NOT transmit any byte!
	fsm.Step(0x1D)
	fsm.Step('r')
	fsm.Step(1)

	select {
	case b := <-fsm.TxChannel():
		t.Fatalf("GS r must NOT transmit when paper is out! Got 0x%02X", b)
	default:
		// Passed! Silent drop per hardware spec
	}
}

func TestFSMMidLineBarcodeAbortQuirk(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// Put some text on current line
	fsm.Step('A')

	// Now send GS k 4 (CODE39)
	fsm.Step(0x1D)
	fsm.Step('k')
	fsm.Step(4)

	// HARDWARE QUIRK: Because line buffer is not empty, GS k aborts and following data is processed as normal text!
	if fsm.state != StateNormal {
		t.Fatalf("expected FSM to abort barcode and stay in StateNormal, got %v", fsm.state)
	}

	// Sending 'B' 'C' 'D' NUL should just be placed into the text buffer
	fsm.Step('B')
	fsm.Step('C')
	if fsm.lineBuf.cursorCol < 3 {
		t.Fatalf("expected cursorCol >= 3, got %d", fsm.lineBuf.cursorCol)
	}
}

func TestFSMSingleLineDoubleWidthAutoCancel(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// Send ESC SO (enable single-line double width)
	fsm.Step(0x1B)
	fsm.Step(0x0E)

	if !reg.SingleLineDoubleW {
		t.Fatal("expected SingleLineDoubleW to be true")
	}

	// Send LF (must automatically clear SingleLineDoubleW)
	fsm.Step(0x0A)

	if reg.SingleLineDoubleW {
		t.Fatal("SingleLineDoubleW must be automatically cleared on LF per hardware spec")
	}
}

func TestFSMGoojprtQR203CommandsAndQuirks(t *testing.T) {
	prof := &mockProfile{}
	reg := NewRegisters(prof)
	head := NewThermalHead(384)
	fsm := NewFSM(prof, reg, head)

	// 1. ESC * m nL nH with valid m=0 (8-dot single density, 2 columns = 2 bytes)
	fsm.Step(0x1B)
	fsm.Step('*')
	fsm.Step(0) // m = 0
	fsm.Step(2) // nL = 2
	fsm.Step(0) // nH = 0
	if fsm.state != StateESCStarData {
		t.Fatalf("expected StateESCStarData, got %v", fsm.state)
	}
	fsm.Step(0xFF)
	fsm.Step(0xAA)
	if fsm.state != StateNormal || !fsm.lineBuf.hasContent {
		t.Fatalf("expected ESC * to complete into lineBuf, state=%v", fsm.state)
	}
	fsm.Step(0x0A) // flush

	// 2. ESC * invalid m quirk (203 User Manual p.18):
	// If m is out of range (e.g. m=99), nL, nH and following data are processed as normal text!
	fsm.Step(0x1B)
	fsm.Step('*')
	fsm.Step(99) // Invalid mode m
	if fsm.state != StateNormal {
		t.Fatalf("expected immediate return to StateNormal on invalid ESC * mode m, got %v", fsm.state)
	}
	fsm.Step('O')
	fsm.Step('K')
	if fsm.lineBuf.cursorCol != 2 {
		t.Fatalf("expected 'O','K' processed as normal text, cursorCol=%d", fsm.lineBuf.cursorCol)
	}
	fsm.Step(0x0A)

	// 3. ESC c 5 n (2-byte parameter sequence: '5' followed by n)
	fsm.Step(0x1B)
	fsm.Step('c')
	fsm.Step('5')
	fsm.Step(0x01) // Disable panel key
	if reg.PanelKeyEnabled {
		t.Fatal("expected PanelKeyEnabled=false after ESC c 5 1")
	}

	// 4. ESC 0 n1 n2 n3 d1..dk (Goojprt Bluetooth baudrate, name, password)
	fsm.Step(0x1B)
	fsm.Step('0')
	fsm.Step(1) // 1 = 19200 bps
	fsm.Step(5) // Name length = 5 ("QR203")
	fsm.Step(4) // Password length = 4 ("1234")
	for _, b := range []byte("QR2031234") {
		fsm.Step(b)
	}
	if reg.BluetoothBaudIndex != 1 || reg.BluetoothName != "QR203" || reg.BluetoothPassword != "1234" {
		t.Fatalf("unexpected ESC 0 state: baud=%d name=%q pass=%q", reg.BluetoothBaudIndex, reg.BluetoothName, reg.BluetoothPassword)
	}

	// 5. ESC ! n Bit 1 (Inverse) and Bit 7 (Underline)
	fsm.Step(0x1B)
	fsm.Step('!')
	fsm.Step(0x82) // Bit 7 (Underline) + Bit 1 (Inverse)
	if !reg.Inverse || reg.Underline != 1 {
		t.Fatalf("expected Inverse=true and Underline=1, got Inverse=%v Underline=%d", reg.Inverse, reg.Underline)
	}
}
