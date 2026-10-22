package printer

import (
	"sync"
	"time"
)

// FSMState represents the current parsing state of the printer MCU.
type FSMState int

const (
	StateNormal FSMState = iota
	StateESC
	StateGS
	StateDC2
	StateFS
	StateParam // Reading fixed-count command parameters
	StateRawBitmap // Consuming binary pixel stream (NO ESCAPES RECOGNIZED)
	StateTabStops
	StateBarcodeFormat1
	StateBarcodeFormat2Len
	StateBarcodeFormat2Data
	StateQRPrefix
	StateQRPayload
	StateUDGHeader
	StateUDGData
	StateGSStarHeader
	StateGSStarData
	StateKanji2nd
)

// FSMEvent is emitted for debugging and live logic analyzer display in the UI.
type FSMEvent struct {
	Byte       byte
	State      FSMState
	Desc       string
	LineHasData bool
	CursorX    int
}

// LineBuffer represents the 2D dot matrix being composed for the current text line.
type LineBuffer struct {
	dotsPerLine int
	height      int       // Current maximum height in dots for this line (24..48)
	matrix      [][]uint8 // [height][dotsPerLine]uint8
	cursorX     int       // Horizontal position in dots (0..384)
	cursorCol   int       // Character column
	hasContent  bool      // Whether any non-space glyph was rendered
}

// NewLineBuffer initializes an empty line buffer.
func NewLineBuffer(dotsPerLine int) *LineBuffer {
	lb := &LineBuffer{
		dotsPerLine: dotsPerLine,
		height:      24,
	}
	lb.Reset()
	return lb
}

func (lb *LineBuffer) Reset() {
	lb.height = 24
	lb.cursorX = 0
	lb.cursorCol = 0
	lb.hasContent = false
	lb.matrix = make([][]uint8, 48) // Pre-allocate up to 48 dots high
	for r := range lb.matrix {
		lb.matrix[r] = make([]uint8, lb.dotsPerLine)
	}
}

// FSM is the byte-driven finite state machine modeling the printer MCU.
type FSM struct {
	mu sync.Mutex

	profile   Profile
	reg       *Registers
	head      *ThermalHead
	lineBuf   *LineBuffer

	state       FSMState
	paramPrefix byte // 'E', 'G', 'D', 'F'
	paramBuf    []byte
	paramNeed   int
	paramCmd    byte

	// Raw bitmap streaming state
	bitmapRemaining   int
	bitmapRowBytes    int
	bitmapRowReceived int
	bitmapCurrentRow  []uint8
	bitmapMSBLeft     bool

	// Barcode temporary state
	barcodeM       int
	barcodeNeed    int
	barcodeBuf     []byte

	// QR temporary state
	qrLenExpected int
	qrFn          byte
	qrPayload     []byte

	// UDG temporary state
	udgY, udgC1, udgC2 byte
	udgCurrentCode     byte
	udgCharWidth       int
	udgDataNeed        int
	udgBuf             []byte

	// GS * temporary state
	gsStarX, gsStarY int
	gsStarNeed       int
	gsStarBuf        []byte

	// Bidirectional Serial TX output buffer
	txOut chan byte

	// Event listener for UI logic analyzer
	eventCallback func(FSMEvent)

	// Sleep tracking
	idleTimer *time.Timer
}

// NewFSM constructs the byte-driven state machine.
func NewFSM(prof Profile, reg *Registers, head *ThermalHead) *FSM {
	fsm := &FSM{
		profile:  prof,
		reg:      reg,
		head:     head,
		lineBuf:  NewLineBuffer(prof.DotsPerLine()),
		state:    StateNormal,
		paramBuf: make([]byte, 0, 16),
		txOut:    make(chan byte, 512),
	}
	return fsm
}

// SetEventCallback registers a callback to trace every processed byte.
func (f *FSM) SetEventCallback(cb func(FSMEvent)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventCallback = cb
}

// TxChannel returns the channel emitting bytes destined for the host serial RX.
func (f *FSM) TxChannel() <-chan byte {
	return f.txOut
}

// Step processes a single raw byte from the serial transport stream.
func (f *FSM) Step(b byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// 1. Sleep & Wakeup Handling
	if f.reg.Sleeping {
		if b == 0xFF {
			// Wake byte received!
			f.reg.Sleeping = false
			f.reg.WakeTime = time.Now()
			f.emit(b, "WAKE from sleep (0xFF received)")
			return
		}
		// In sleep mode, any byte other than 0xFF is ignored!
		f.emit(b, "IGNORED (Printer is asleep)")
		return
	}

	// Wakeup stabilization window: physical printer drops bytes sent within 50ms of wake!
	if !f.reg.WakeTime.IsZero() && time.Since(f.reg.WakeTime) < 50*time.Millisecond {
		f.emit(b, "DROPPED (Within 50ms post-wake stabilization window)")
		return
	}

	// 2. State Machine Dispatch
	switch f.state {
	case StateNormal:
		f.handleNormal(b)

	case StateESC:
		f.handleESC(b)

	case StateGS:
		f.handleGS(b)

	case StateDC2:
		f.handleDC2(b)

	case StateFS:
		f.handleFS(b)

	case StateParam:
		f.handleParam(b)

	case StateRawBitmap:
		f.handleRawBitmap(b)

	case StateTabStops:
		f.handleTabStops(b)

	case StateBarcodeFormat1:
		f.handleBarcodeFormat1(b)

	case StateBarcodeFormat2Len:
		f.barcodeNeed = int(b)
		f.barcodeBuf = make([]byte, 0, f.barcodeNeed)
		if f.barcodeNeed == 0 {
			f.state = StateNormal
		} else {
			f.state = StateBarcodeFormat2Data
		}
		f.emit(b, "BARCODE Fmt2 Length")

	case StateBarcodeFormat2Data:
		f.barcodeBuf = append(f.barcodeBuf, b)
		if len(f.barcodeBuf) >= f.barcodeNeed {
			f.renderBarcode()
			f.state = StateNormal
		}
		f.emit(b, "BARCODE Data")

	case StateQRPrefix:
		f.handleQRPrefix(b)

	case StateQRPayload:
		f.qrPayload = append(f.qrPayload, b)
		if len(f.qrPayload) >= f.qrLenExpected {
			f.executeQR()
			f.state = StateNormal
		}
		f.emit(b, "QR Payload")

	case StateUDGHeader:
		f.handleUDGHeader(b)

	case StateUDGData:
		f.handleUDGData(b)

	case StateGSStarHeader:
		f.handleGSStarHeader(b)

	case StateGSStarData:
		f.gsStarBuf = append(f.gsStarBuf, b)
		if len(f.gsStarBuf) >= f.gsStarNeed {
			f.reg.DownloadedImage = &DownloadedBitImage{
				WidthDots:  f.gsStarX * 8,
				HeightDots: f.gsStarY * 8,
				Data:       f.gsStarBuf,
			}
			f.state = StateNormal
		}
		f.emit(b, "GS * Image Data")

	case StateKanji2nd:
		// Render multi-byte Kanji character
		c1 := f.paramCmd
		c2 := b
		glyph := f.profile.RenderKanji(c1, c2)
		f.renderGlyphIntoLine(glyph, 24, 24)
		f.state = StateNormal
		f.emit(b, "KANJI 2nd Byte")
	}
}

func (f *FSM) handleNormal(b byte) {
	// If offline, ignore non-control print data
	if !f.reg.Online && b != 0x1B && b != 0x1D && b != 0x12 {
		f.emit(b, "IGNORED (Printer Offline)")
		return
	}

	switch b {
	case 0x1B: // ESC
		f.state = StateESC
		f.emit(b, "ESC Prefix")

	case 0x1D: // GS
		f.state = StateGS
		f.emit(b, "GS Prefix")

	case 0x12: // DC2
		f.state = StateDC2
		f.emit(b, "DC2 Prefix")

	case 0x1C: // FS
		f.state = StateFS
		f.emit(b, "FS Prefix")

	case 0x0A: // LF
		f.flushLineBuffer()
		f.head.FeedRows(f.reg.LineSpacing, f.reg.ResetLineDoubleWidth())
		f.emit(b, "LF (Line Feed)")

	case 0x0D: // CR
		// Serial model quirk: ignored if automatic line feed is disabled
		f.emit(b, "CR (Carriage Return)")

	case 0x09: // HT (Horizontal Tab)
		f.handleHT()
		f.emit(b, "HT (Horizontal Tab)")

	case 0x0C: // FF
		f.flushLineBuffer()
		f.emit(b, "FF (Form Feed)")

	default:
		// Printable character
		if f.reg.KanjiMode && (b >= 0x81 && b <= 0xFE) {
			f.paramCmd = b
			f.state = StateKanji2nd
			f.emit(b, "KANJI 1st Byte")
			return
		}

		f.renderPrintableChar(b)
		f.emit(b, "PRINT CHAR")
	}
}

func (f *FSM) handleESC(b byte) {
	f.paramPrefix = 'E'
	f.paramCmd = b
	switch b {
	case '@': // 0x40 Initialize printer
		f.reg.Reset(f.profile)
		f.lineBuf.Reset()
		f.state = StateNormal
		f.emit(b, "ESC @ (Initialize)")

	case '!', '3', 'a', 'J', 'd', 'E', 'G', '-', '{', 'V', 'R', 't', ' ', 'B', '=', 'c', '%', '?':
		// 1-parameter commands
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC Cmd (1 param needed)")

	case '7': // Setting Control Parameter (ESC 7 n1 n2 n3)
		f.paramNeed = 3
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC 7 (3 params needed)")

	case '8': // Sleep parameter (ESC 8 n1 [n2])
		if f.profile.FirmwareVersion() == FirmwareV216 {
			f.paramNeed = 1 // v2.16 quirk: 1 byte
		} else {
			f.paramNeed = 2 // v2.64+ quirk: 2 bytes
		}
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC 8 (Sleep param)")

	case '$': // Absolute print position (ESC $ nL nH)
		f.paramNeed = 2
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC $ (2 params needed)")

	case '2': // Select default line spacing
		f.reg.LineSpacing = int(f.profile.DefaultLineSpacing())
		f.state = StateNormal
		f.emit(b, "ESC 2 (Default line spacing)")

	case 0x0E: // ESC SO: Select Double Width Mode (for current line only)
		f.reg.SingleLineDoubleW = true
		f.state = StateNormal
		f.emit(b, "ESC SO (Single-line Double Width ON)")

	case 0x14: // ESC DC4: Disable Double Width Mode
		f.reg.SingleLineDoubleW = false
		f.state = StateNormal
		f.emit(b, "ESC DC4 (Single-line Double Width OFF)")

	case 'D': // Set horizontal tab positions (ESC D n1..nk NUL)
		f.paramBuf = f.paramBuf[:0]
		f.state = StateTabStops
		f.emit(b, "ESC D (Tab stops)")

	case '*': // Select bit-image mode (ESC * m nL nH d...)
		f.paramNeed = 3
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC * (Bit image header)")

	case '&': // Define user-defined characters (ESC & y c1 c2...)
		f.paramNeed = 3
		f.paramBuf = f.paramBuf[:0]
		f.state = StateUDGHeader
		f.emit(b, "ESC & (Define UDG header)")

	case 'v': // Transmit paper sensor status (ESC v n)
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "ESC v (Status query)")

	default:
		// Unknown ESC command: return to normal
		f.state = StateNormal
		f.emit(b, "UNKNOWN ESC Cmd")
	}
}

func (f *FSM) handleGS(b byte) {
	f.paramPrefix = 'G'
	f.paramCmd = b
	switch b {
	case '!', 'B', 'H', 'h', 'w', 'x', 'a', 'r', '/':
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "GS Cmd (1 param needed)")

	case 'L': // Set left margin (GS L nL nH)
		f.paramNeed = 2
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "GS L (Left margin)")

	case 'v': // GS v 0 m xL xH yL yH
		f.paramNeed = 6 // '0', m, xL, xH, yL, yH
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "GS v (Raster bitmap header)")

	case '*': // Define downloaded bit image (GS * x y d...)
		f.paramNeed = 2
		f.paramBuf = f.paramBuf[:0]
		f.state = StateGSStarHeader
		f.emit(b, "GS * (Download image header)")

	case 'k': // Barcode (GS k m ...)
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "GS k (Barcode prefix)")

	case '(': // 2D QR Code prefix (GS ( k ...)
		f.paramNeed = 2 // expect 'k' and pL
		f.paramBuf = f.paramBuf[:0]
		f.state = StateQRPrefix
		f.emit(b, "GS ( (2D symbol prefix)")

	default:
		f.state = StateNormal
		f.emit(b, "UNKNOWN GS Cmd")
	}
}

func (f *FSM) handleDC2(b byte) {
	f.paramPrefix = 'D'
	f.paramCmd = b
	switch b {
	case '#': // Set printing density (DC2 # n)
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "DC2 # (Density param)")

	case 'T': // Print test page (DC2 T)
		f.flushLineBuffer()
		rows := f.profile.GenerateSelfTestPage(f.reg)
		for _, r := range rows {
			f.head.BurnRow(r, f.reg)
		}
		f.state = StateNormal
		f.emit(b, "DC2 T (Self-Test Ticket Printed)")

	case '*': // Print chunk bitmap (DC2 * r n [d...])
		f.paramNeed = 2 // r, n
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "DC2 * (Chunk bitmap header)")

	case 'V', 'v': // Print MSB / LSB full-width bitmap (DC2 V/v nL nH)
		f.paramNeed = 2
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "DC2 V/v (Full-width bitmap header)")

	default:
		f.state = StateNormal
		f.emit(b, "UNKNOWN DC2 Cmd")
	}
}

func (f *FSM) handleFS(b byte) {
	f.paramPrefix = 'F'
	f.paramCmd = b
	switch b {
	case '&': // Select Kanji character mode
		f.reg.KanjiMode = true
		f.state = StateNormal
		f.emit(b, "FS & (Kanji Mode ON)")

	case '.': // Cancel Kanji character mode
		f.reg.KanjiMode = false
		f.state = StateNormal
		f.emit(b, "FS . (Kanji Mode OFF)")

	case '!': // Set Kanji print mode (FS ! n)
		f.paramNeed = 1
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
		f.emit(b, "FS ! (Kanji mode param)")

	default:
		f.state = StateNormal
		f.emit(b, "UNKNOWN FS Cmd")
	}
}

func (f *FSM) handleParam(b byte) {
	f.paramBuf = append(f.paramBuf, b)
	if len(f.paramBuf) < f.paramNeed {
		f.emit(b, "Param Byte")
		return
	}

	// Parameters collected, execute command
	f.executeParamCommand()
}

func (f *FSM) executeParamCommand() {
	p := f.paramBuf
	cmd := f.paramCmd

	switch f.paramPrefix {
	case 'E':
		f.executeEscParam(cmd, p)
	case 'G':
		f.executeGsParam(cmd, p)
	case 'D':
		f.executeDc2Param(cmd, p)
	case 'F':
		f.executeFsParam(cmd, p)
	}

	if f.state == StateParam {
		f.state = StateNormal
		f.emit(p[len(p)-1], "Command Executed")
	}
}

func (f *FSM) executeEscParam(cmd byte, p []byte) {
	switch cmd {
	case '!': // ESC ! n
		f.applyEscExclamation(p[0])
	case '3': // ESC 3 n
		f.reg.LineSpacing = int(p[0])
	case 'a': // ESC a n
		// QUIRK: Enabled only at beginning of line!
		if f.lineBuf.cursorCol == 0 {
			f.reg.Justify = int(p[0] % 3)
		}
	case 'J': // ESC J n
		f.flushLineBuffer()
		f.head.FeedRows(int(p[0]), f.reg)
	case 'd': // ESC d n
		f.flushLineBuffer()
		f.head.FeedRows(int(p[0])*f.reg.LineSpacing, f.reg)
	case 'E': // ESC E n: Bold
		f.reg.Bold = (p[0] & 1) != 0
	case 'G': // ESC G n: Double-strike
		f.reg.DoubleStrike = (p[0] & 1) != 0
	case '-': // ESC - n: Underline
		f.reg.Underline = int(p[0] % 3)
	case '{': // ESC { n: Upside-down
		if f.lineBuf.cursorCol == 0 {
			f.reg.UpsideDown = (p[0] & 1) != 0
		}
	case 'V': // ESC V n: Rotate 90
		f.reg.Rotate90 = (p[0] & 1) != 0
	case 'R': // ESC R n: Intl Charset
		f.reg.IntlCharset = int(p[0] & 0x0F)
	case 't': // ESC t n: Code Page
		f.reg.CodePage = int(p[0])
	case ' ': // ESC SP n: Right spacing
		f.reg.RightSideSpacing = int(p[0])
	case 'B': // ESC B n: Left blank chars
		f.reg.LeftBlankChars = int(p[0])
	case '=': // ESC = n: Online/Offline
		f.reg.Online = (p[0] & 1) != 0
	case 'c': // ESC c 5 n: Panel key enable
		f.reg.PanelKeyEnabled = (p[0] & 1) == 0
	case '%': // ESC % n: User chars enable
		f.reg.UserCharsEnabled = (p[0] & 1) != 0
	case '?': // ESC ? n: Cancel UDG char
		delete(f.reg.UserDefinedChars, p[0])
	case '7': // ESC 7 n1 n2 n3
		f.reg.MaxHeatingDots = p[0]
		f.reg.HeatingTime = p[1]
		f.reg.HeatingInterval = p[2]
	case '8': // ESC 8 sleep
		if len(p) >= 2 {
			f.reg.SleepTimeoutSec = int(p[0]) + int(p[1])*256
		} else {
			f.reg.SleepTimeoutSec = int(p[0])
		}
	case '$': // ESC $ nL nH
		f.reg.AbsoluteX = int(p[0]) + int(p[1])*256
	case 'v': // ESC v n: Status command
		f.transmitStatusESCv()
	}
}

func (f *FSM) executeGsParam(cmd byte, p []byte) {
	switch cmd {
	case '!': // GS ! n
		heightMult := int(p[0]&0x07) + 1
		widthMult := int((p[0]>>4)&0x07) + 1
		f.reg.DoubleHeight = heightMult > 1
		f.reg.DoubleWidth = widthMult > 1
	case 'B': // GS B n: Inverse
		f.reg.Inverse = (p[0] & 1) != 0
	case 'L': // GS L nL nH: Left margin
		if f.lineBuf.cursorCol == 0 {
			f.reg.LeftMargin = int(p[0]) + int(p[1])*256
		}
	case 'r': // GS r n: Transmit status
		f.transmitStatusGSr()
	case 'a': // GS a n: ASB & RTS
		f.reg.ASBEnabled = (p[0] & 0x04) != 0
		f.reg.RTSFlowControl = (p[0] & 0x20) != 0
	case 'H': // GS H n: Barcode HRI position
		f.reg.BarcodeHRIPosition = int(p[0] % 4)
	case 'h': // GS h n: Barcode height
		f.reg.BarcodeHeight = int(p[0])
	case 'w': // GS w n: Barcode width
		f.reg.BarcodeWidth = int(p[0])
	case 'x': // GS x n: Barcode left space
		f.reg.BarcodeLeftSpace = int(p[0])
	case '/': // GS / n: Print downloaded bit image
		f.printDownloadedImage(p[0])
	case 'k': // GS k m: Barcode
		f.barcodeM = int(p[0])
		// QUIRK: If line buffer is NOT empty, abort barcode command and dump payload as text!
		if f.lineBuf.hasContent {
			f.state = StateNormal // Processes following bytes as normal text
			f.emit(p[0], "BARCODE ABORT (Line buffer not empty - dump as text quirk)")
			return
		}
		if f.barcodeM <= 10 {
			f.barcodeBuf = f.barcodeBuf[:0]
			f.state = StateBarcodeFormat1
		} else {
			f.state = StateBarcodeFormat2Len
		}
	case 'v': // GS v 0 m xL xH yL yH: Raster bitmap
		f.startGSv0Bitmap()
	}
}

func (f *FSM) executeDc2Param(cmd byte, p []byte) {
	switch cmd {
	case '#': // DC2 # n
		f.reg.PrintDensity = p[0] & 0x1F
		f.reg.PrintBreakTime = (p[0] >> 5) & 0x07
	case '*': // DC2 * r n: Chunk bitmap
		f.startDC2StarBitmap()
	case 'V', 'v': // DC2 V/v nL nH: Full-width bitmap
		f.startDC2VBitmap(cmd == 'V')
	}
}

func (f *FSM) executeFsParam(cmd byte, p []byte) {
	switch cmd {
	case '!': // FS ! n: Kanji mode
		f.reg.KanjiDoubleW = (p[0] & 0x04) != 0
		f.reg.KanjiDoubleH = (p[0] & 0x08) != 0
		f.reg.KanjiUnderline = (p[0] & 0x80) != 0
	}
}

func (f *FSM) renderBarcode() {
	res := f.profile.GenerateBarcode(f.barcodeM, f.barcodeBuf, f.reg)
	if res != nil {
		for _, r := range res.Rows {
			f.head.BurnRow(r, f.reg)
		}
	}
}

func (f *FSM) executeQR() {
	rows := f.profile.ProcessQRCommand(f.qrFn, f.qrPayload, f.reg)
	if len(rows) > 0 {
		for _, r := range rows {
			f.head.BurnRow(r, f.reg)
		}
	}
}

func (f *FSM) applyEscExclamation(n byte) {
	if (n & 0x01) != 0 {
		f.reg.Font = FontB
	} else {
		f.reg.Font = FontA
	}

	// v2.16 firmware used Bit 1 for Inverse
	if f.profile.FirmwareVersion() == FirmwareV216 {
		f.reg.Inverse = (n & 0x02) != 0
	}

	f.reg.UpsideDown = (n & 0x04) != 0
	f.reg.Bold = (n & 0x08) != 0
	f.reg.DoubleHeight = (n & 0x10) != 0
	f.reg.DoubleWidth = (n & 0x20) != 0
	f.reg.DeleteLine = (n & 0x40) != 0
}

func (f *FSM) handleHT() {
	// Find next tab stop in character columns
	nextCol := 32
	for _, tab := range f.reg.TabStops {
		if tab > f.lineBuf.cursorCol {
			nextCol = tab
			break
		}
	}
	f.lineBuf.cursorCol = nextCol
	charWidth := 12
	if f.reg.Font == FontB {
		charWidth = 9
	}
	f.lineBuf.cursorX = nextCol * charWidth
	if f.lineBuf.cursorX >= 384 {
		f.flushLineBuffer()
	}
}

func (f *FSM) handleTabStops(b byte) {
	if b == 0x00 {
		f.state = StateNormal
		f.emit(b, "ESC D Tab Stops Terminated (NUL)")
		return
	}
	// Transmit in ascending order. If n_k <= n_{k-1}, finish tab setting
	// QUIRK: Following bytes are processed as normal text!
	if len(f.paramBuf) > 0 && b <= f.paramBuf[len(f.paramBuf)-1] {
		f.state = StateNormal
		f.handleNormal(b)
		return
	}
	f.paramBuf = append(f.paramBuf, b)
	if len(f.paramBuf) >= 32 {
		f.state = StateNormal
	}
	f.reg.TabStops = make([]int, len(f.paramBuf))
	for i, v := range f.paramBuf {
		f.reg.TabStops[i] = int(v)
	}
	f.emit(b, "ESC D Tab Stop Set")
}

// --------------------------------------------------------------------------
// Raw Bitmap Consuming Engine (BLIND BYTE COUNT LOCKUP QUIRK)
// --------------------------------------------------------------------------

func (f *FSM) startDC2StarBitmap() {
	r := int(f.paramBuf[0]) // Rows high
	n := int(f.paramBuf[1]) // Bytes wide (1..48)
	if n > 48 {
		n = 48
	}
	f.bitmapRemaining = r * n
	f.bitmapRowBytes = n
	f.bitmapRowReceived = 0
	f.bitmapCurrentRow = make([]uint8, 384)
	f.bitmapMSBLeft = true
	f.state = StateRawBitmap
	f.emit(f.paramBuf[1], "DC2 * Stream Start")
}

func (f *FSM) startDC2VBitmap(msbLeft bool) {
	height := int(f.paramBuf[0]) + int(f.paramBuf[1])*256
	f.bitmapRemaining = 48 * height
	f.bitmapRowBytes = 48
	f.bitmapRowReceived = 0
	f.bitmapCurrentRow = make([]uint8, 384)
	f.bitmapMSBLeft = msbLeft
	f.state = StateRawBitmap
	f.emit(f.paramBuf[1], "DC2 V/v Stream Start")
}

func (f *FSM) startGSv0Bitmap() {
	p := f.paramBuf
	// p[0] = '0', p[1] = m, p[2..3] = xL, xH, p[4..5] = yL, yH
	widthBytes := int(p[2]) + int(p[3])*256
	heightDots := int(p[4]) + int(p[5])*256
	if widthBytes > 48 {
		widthBytes = 48
	}
	f.bitmapRemaining = widthBytes * heightDots
	f.bitmapRowBytes = widthBytes
	f.bitmapRowReceived = 0
	f.bitmapCurrentRow = make([]uint8, 384)
	f.bitmapMSBLeft = true
	f.state = StateRawBitmap
	f.emit(p[5], "GS v 0 Stream Start")
}

func (f *FSM) handleRawBitmap(b byte) {
	// Consuming raw pixel bits: NO ESCAPE POSSIBLE until remaining reaches 0!
	f.bitmapRemaining--

	// Unpack byte into dots for the current row
	startDot := f.bitmapRowReceived * 8
	for bit := 0; bit < 8; bit++ {
		dotPos := startDot + bit
		if dotPos < 384 {
			var isSet bool
			if f.bitmapMSBLeft {
				isSet = (b & (1 << (7 - bit))) != 0
			} else {
				isSet = (b & (1 << bit)) != 0 // LSB left
			}
			if isSet {
				f.bitmapCurrentRow[dotPos] = 1
			}
		}
	}
	f.bitmapRowReceived++

	// When row is full, burn row to thermal head
	if f.bitmapRowReceived >= f.bitmapRowBytes {
		f.head.BurnRow(f.bitmapCurrentRow, f.reg)
		f.bitmapCurrentRow = make([]uint8, 384)
		f.bitmapRowReceived = 0
	}

	if f.bitmapRemaining <= 0 {
		f.state = StateNormal
		f.emit(b, "Bitmap Stream Complete")
	} else {
		f.emit(b, "Bitmap Byte Consumed")
	}
}

// --------------------------------------------------------------------------
// Barcode & QR Handling
// --------------------------------------------------------------------------

func (f *FSM) handleBarcodeFormat1(b byte) {
	if b == 0x00 {
		f.renderBarcode()
		f.state = StateNormal
		f.emit(b, "Barcode Fmt1 Complete (NUL)")
		return
	}
	f.barcodeBuf = append(f.barcodeBuf, b)
	// Check standard length limits (e.g. 12 for UPC-A)
	if (f.barcodeM == 0 || f.barcodeM == 1) && len(f.barcodeBuf) >= 12 {
		f.renderBarcode()
		f.state = StateNormal
		f.emit(b, "Barcode Fmt1 Complete (Auto-limit)")
	}
}

func (f *FSM) handleQRPrefix(b byte) {
	f.paramBuf = append(f.paramBuf, b)
	// Expect 'k', pL, pH, cn, fn
	if len(f.paramBuf) == 5 {
		pL := int(f.paramBuf[1])
		pH := int(f.paramBuf[2])
		f.qrLenExpected = (pL + pH*256) - 2 // minus cn and fn
		f.qrFn = f.paramBuf[4]
		f.qrPayload = make([]byte, 0, f.qrLenExpected)
		f.state = StateQRPayload
	}
}

// --------------------------------------------------------------------------
// User Defined Characters & Downloaded Bit Image
// --------------------------------------------------------------------------

func (f *FSM) handleUDGHeader(b byte) {
	f.paramBuf = append(f.paramBuf, b)
	if len(f.paramBuf) == 3 {
		f.udgY = f.paramBuf[0]
		f.udgC1 = f.paramBuf[1]
		f.udgC2 = f.paramBuf[2]
		f.udgCurrentCode = f.udgC1
		f.paramNeed = 1 // Read character width x
		f.paramBuf = f.paramBuf[:0]
		f.state = StateParam
	}
}

func (f *FSM) handleUDGData(b byte) {
	f.udgBuf = append(f.udgBuf, b)
	if len(f.udgBuf) >= f.udgDataNeed {
		f.reg.UserDefinedChars[f.udgCurrentCode] = &UserDefinedChar{
			Width: f.udgCharWidth,
			Data:  f.udgBuf,
		}
		if f.udgCurrentCode < f.udgC2 {
			f.udgCurrentCode++
			f.state = StateUDGHeader // Read next width
		} else {
			f.state = StateNormal
		}
	}
}

func (f *FSM) handleGSStarHeader(b byte) {
	f.paramBuf = append(f.paramBuf, b)
	if len(f.paramBuf) == 2 {
		f.gsStarX = int(f.paramBuf[0])
		f.gsStarY = int(f.paramBuf[1])
		f.gsStarNeed = f.gsStarX * f.gsStarY * 8
		f.gsStarBuf = make([]byte, 0, f.gsStarNeed)
		// Defining downloaded image clears UDG per manual
		f.reg.UserDefinedChars = make(map[byte]*UserDefinedChar)
		f.state = StateGSStarData
	}
}

func (f *FSM) printDownloadedImage(m byte) {
	img := f.reg.DownloadedImage
	if img == nil {
		return // Ignored if not defined
	}
	f.flushLineBuffer()
	// Render image rows
	rows := img.HeightDots
	bytesPerRow := img.WidthDots / 8
	for r := 0; r < rows; r++ {
		rowDots := make([]uint8, 384)
		for b := 0; b < bytesPerRow; b++ {
			byteVal := img.Data[r*bytesPerRow+b]
			for bit := 0; bit < 8; bit++ {
				pos := b*8 + bit
				if pos < 384 && (byteVal&(1<<(7-bit))) != 0 {
					rowDots[pos] = 1
				}
			}
		}
		f.head.BurnRow(rowDots, f.reg)
	}
}

// --------------------------------------------------------------------------
// Status Queries & Bidirectional Feedback
// --------------------------------------------------------------------------

func (f *FSM) transmitStatusESCv() {
	if f.profile.FirmwareVersion() == FirmwareV216 {
		// v2.16 ASCII format: P<Paper>V<Voltage>T<Degree>
		paper := "1"
		if f.reg.PaperState == PaperOut {
			paper = "0"
		}
		str := "P" + paper + "V72T30"
		for i := 0; i < len(str); i++ {
			f.sendTx(str[i])
		}
		return
	}

	// v2.64+ Binary format: 1 byte
	// Bit 0: Online (1)
	// Bit 2: Paper Out (4)
	// Bit 3: Voltage > 9.5V (8)
	// Bit 6: Temp > 60°C (64)
	var status byte = 0
	if f.reg.Online {
		status |= 0x01
	}
	if f.reg.PaperState == PaperOut {
		status |= 0x04
	}
	if f.reg.SupplyVoltage > 9.5 {
		status |= 0x08
	}
	if f.reg.HeadTemperature > 60.0 {
		status |= 0x40
	}
	f.sendTx(status)
}

func (f *FSM) transmitStatusGSr() {
	// QUIRK: When paper end sensor detects paper out, printer goes offline and does NOT execute GS r!
	if f.reg.PaperState == PaperOut {
		f.reg.Online = false
		return // Silent drop per manual
	}

	var status byte = 0x00 // Adequate
	if f.reg.PaperState == PaperNearEnd {
		status = 0x0C // Paper near end
	}
	f.sendTx(status)
}

func (f *FSM) sendTx(b byte) {
	select {
	case f.txOut <- b:
	default:
		// Drop if host is not reading TX
	}
}

// --------------------------------------------------------------------------
// Glyph Rendering & Line Buffer Management
// --------------------------------------------------------------------------

func (f *FSM) renderPrintableChar(b byte) {
	// Look up character glyph
	var glyph []uint8
	charWidth := 12
	charHeight := 24

	if f.reg.UserCharsEnabled {
		if udg, ok := f.reg.UserDefinedChars[b]; ok {
			glyph = make([]uint8, udg.Width*24)
			// Unpack 3-byte columns into rows
			for col := 0; col < udg.Width; col++ {
				for byteRow := 0; byteRow < 3; byteRow++ {
					bVal := udg.Data[col*3+byteRow]
					for bit := 0; bit < 8; bit++ {
						y := byteRow*8 + bit
						if (bVal & (1 << (7 - bit))) != 0 {
							glyph[y*udg.Width+col] = 1
						}
					}
				}
			}
			charWidth = udg.Width
		}
	}

	if glyph == nil {
		if f.reg.Font == FontB {
			charWidth = 9
			charHeight = 17
		}
		glyph = f.profile.RenderChar(b, f.reg.Font, f.reg.CodePage, f.reg.IntlCharset)
	}

	// Apply Double Width / Double Height multipliers
	isDoubleW := f.reg.DoubleWidth || f.reg.SingleLineDoubleW
	isDoubleH := f.reg.DoubleHeight

	renderW := charWidth
	if isDoubleW {
		renderW *= 2
	}
	renderH := charHeight
	if isDoubleH {
		renderH *= 2
	}

	// Line buffer wrap check
	if f.lineBuf.cursorX+renderW > 384 {
		f.flushLineBuffer()
		f.head.FeedRows(f.reg.LineSpacing, f.reg)
	}

	f.renderGlyphIntoLine(glyph, charWidth, charHeight)
	f.lineBuf.cursorCol++
}

func (f *FSM) renderGlyphIntoLine(glyph []uint8, srcW, srcH int) {
	isDoubleW := f.reg.DoubleWidth || f.reg.SingleLineDoubleW
	isDoubleH := f.reg.DoubleHeight

	destW := srcW
	if isDoubleW {
		destW *= 2
	}
	destH := srcH
	if isDoubleH {
		destH *= 2
	}

	if destH > f.lineBuf.height {
		f.lineBuf.height = destH
	}

	startX := f.lineBuf.cursorX + f.reg.LeftMargin
	for dy := 0; dy < destH; dy++ {
		sy := dy
		if isDoubleH {
			sy = dy / 2
		}
		for dx := 0; dx < destW; dx++ {
			sx := dx
			if isDoubleW {
				sx = dx / 2
			}
			var dot uint8
			if sy < srcH && sx < srcW {
				dot = glyph[sy*srcW+sx]
			}

			// Apply bold / emphasized (horizontal smear)
			if f.reg.Bold || f.reg.DoubleStrike {
				if sx > 0 && glyph[sy*srcW+sx-1] > 0 {
					dot = 1
				}
			}

			// Apply underline (rows 22-23 for Font A)
			if f.reg.Underline > 0 && dy >= destH-f.reg.Underline {
				dot = 1
			}

			// Apply strikethrough / deleteline
			if f.reg.DeleteLine && dy == destH/2 {
				dot = 1
			}

			// Apply inverse (reverse white/black)
			if f.reg.Inverse {
				if dot == 0 {
					dot = 1
				} else {
					dot = 0
				}
			}

			posX := startX + dx
			if posX < 384 {
				f.lineBuf.matrix[dy][posX] = dot
				if dot > 0 {
					f.lineBuf.hasContent = true
				}
			}
		}
	}

	f.lineBuf.cursorX += destW + f.reg.RightSideSpacing
}

func (f *FSM) flushLineBuffer() {
	if !f.lineBuf.hasContent {
		f.lineBuf.Reset()
		return
	}

	// Burn each active row of the line buffer to the thermal head
	for r := 0; r < f.lineBuf.height; r++ {
		row := f.lineBuf.matrix[r]
		f.head.BurnRow(row, f.reg)
	}

	f.lineBuf.Reset()
}

func (f *FSM) emit(b byte, desc string) {
	if f.eventCallback != nil {
		f.eventCallback(FSMEvent{
			Byte:       b,
			State:      f.state,
			Desc:       desc,
			LineHasData: f.lineBuf.hasContent,
			CursorX:    f.lineBuf.cursorX,
		})
	}
}

// ResetLineDoubleWidth clears the single-line double-width flag on LF.
func (r *Registers) ResetLineDoubleWidth() *Registers {
	r.SingleLineDoubleW = false
	return r
}
