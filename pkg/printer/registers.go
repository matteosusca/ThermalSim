package printer

import (
	"time"
)

// UserDefinedChar holds custom 24-dot tall bitmap data for a character code.
type UserDefinedChar struct {
	Width int    // Horizontal dots (0..12 for Font A)
	Data  []byte // 3 * Width bytes (column format)
}

// DownloadedBitImage holds an image stored in volatile RAM via GS *.
type DownloadedBitImage struct {
	WidthDots  int // x * 8
	HeightDots int // y * 8
	Data       []byte
}

// NVBitImage holds an image stored in non-volatile flash via FS q.
type NVBitImage struct {
	WidthDots  int
	HeightDots int
	Data       []byte
}

// Registers encapsulates the firmware state registers of the printer.
type Registers struct {
	// Operational State
	Online          bool
	Sleeping        bool
	SleepTimeoutSec int
	WakeTime        time.Time

	// Formatting Modes
	Font               FontType
	Bold               bool
	DoubleStrike       bool
	DoubleWidth        bool
	DoubleHeight       bool
	WidthMultiplier    int // GS ! horizontal scale (1..8)
	HeightMultiplier   int // GS ! vertical scale (1..8)
	Underline          int // 0 = off, 1 = 1-dot, 2 = 2-dot
	DeleteLine         bool
	UpsideDown         bool
	Inverse            bool
	Rotate90           bool
	SingleLineDoubleW  bool // Enabled by ESC SO; cleared automatically on LF or DC4
	RightSideSpacing   int  // ESC SP n (dots)

	// Layout & Spacing
	LineSpacing  int   // Total dot rows fed on line print (default 30 or 32)
	Justify      int   // 0 = Left, 1 = Center, 2 = Right
	LeftMargin   int   // GS L (dots)
	LeftBlankChars int // ESC B n
	AbsoluteX    int   // ESC $ (dots)
	TabStops     []int // ESC D tab stops (character column intervals)

	// Encodings & Localization
	CodePage    int // ESC t n
	IntlCharset int // ESC R n
	KanjiMode   bool
	KanjiDoubleW bool
	KanjiDoubleH bool
	KanjiUnderline bool
	ChineseFormat int // 0 = GBK, 1 = UTF-8, 3 = BIG5

	// Thermal Head & Power Parameters (ESC 7 & DC2 #)
	MaxHeatingDots  uint8 // n1: max dots = 8 * (n1 + 1). Default 7 (64 dots)
	HeatingTime     uint8 // n2: time = n2 * 10 us. Default 80 (800 us)
	HeatingInterval uint8 // n3: interval = n3 * 10 us. Default 2 (20 us)
	PrintDensity    uint8 // DC2 # D4..D0 (50% + 5% * n)
	PrintBreakTime  uint8 // DC2 # D7..D5 (n * 250 us)

	// Barcode Parameters (GS H, GS h, GS w, GS x)
	BarcodeHRIPosition int // 0 = none, 1 = above, 2 = below, 3 = both
	BarcodeHeight      int // 1..255 dots (default 50 or 162)
	BarcodeWidth       int // 2..6 module width
	BarcodeLeftSpace   int // GS x left space

	// QR Code temporary symbol storage (GS ( k)
	QRModel        int    // 1 or 2
	QRModuleSize   int    // 1..16 dots (default 3)
	QRErrorLevel   int    // 48=L, 49=M, 50=Q, 51=H
	QRSymbolBuffer []byte // Payload stored via Fn 80

	// Hardware Sensors & Virtual Switchboard
	PaperState       PaperStatus
	PlatenOpen       bool
	SupplyVoltage    float64 // Volts (default 7.2V; >9.5V triggers fault; <4.5V brownout)
	HeadTemperature  float64 // Celsius (default 25°C; >60°C triggers overheat error)
	PanelKeyEnabled  bool    // ESC c 5
	ASBEnabled       bool    // GS a bit 2
	RTSFlowControl   bool    // GS a bit 5

	// Memory Storage
	UserDefinedChars map[byte]*UserDefinedChar // Defined via ESC &
	UserCharsEnabled bool                      // ESC %
	DownloadedImage  *DownloadedBitImage       // Defined via GS *
	NVBitmaps        map[int]*NVBitImage       // Defined via FS q

	// Goojprt QR203 Bluetooth / UART Configuration (ESC 0 n1 n2 n3 d1..dk)
	BluetoothBaudIndex uint8
	BluetoothName      string
	BluetoothPassword  string
}

// NewRegisters initializes a Registers struct with hardware defaults for the profile.
func NewRegisters(prof Profile) *Registers {
	r := &Registers{
		Online:             true,
		SupplyVoltage:      7.2,
		HeadTemperature:    25.0,
		PanelKeyEnabled:    true,
		UserDefinedChars:   make(map[byte]*UserDefinedChar),
		NVBitmaps:          make(map[int]*NVBitImage),
		BarcodeHRIPosition: 0,
		BarcodeHeight:      50,
		BarcodeWidth:       3,
		QRModel:            2,
		QRModuleSize:       3,
		QRErrorLevel:       49, // Level M
	}
	r.Reset(prof)
	return r
}

// Reset restores default settings per ESC @ (clears buffers & formatting, standard mode).
// Note: Receive buffer is NOT cleared by ESC @ per the hardware manual.
func (r *Registers) Reset(prof Profile) {
	r.Online = true
	r.Font = FontA
	r.Bold = false
	r.DoubleStrike = false
	r.DoubleWidth = false
	r.DoubleHeight = false
	r.WidthMultiplier = 1
	r.HeightMultiplier = 1
	r.Underline = 0
	r.DeleteLine = false
	r.UpsideDown = false
	r.Inverse = false
	r.Rotate90 = false
	r.SingleLineDoubleW = false
	r.RightSideSpacing = 0

	r.LineSpacing = int(prof.DefaultLineSpacing())
	r.Justify = 0
	r.LeftMargin = 0
	r.LeftBlankChars = 0
	r.AbsoluteX = 0

	// Default tab stops: every 8 Font A characters (columns 8, 16, 24...)
	r.TabStops = []int{8, 16, 24}

	r.CodePage = 0
	r.IntlCharset = 0
	r.KanjiMode = false
	r.KanjiDoubleW = false
	r.KanjiDoubleH = false
	r.KanjiUnderline = false
	r.ChineseFormat = 0

	r.MaxHeatingDots = prof.MaxHeatingDots()
	r.HeatingTime = prof.DefaultHeatingTime()
	r.HeatingInterval = prof.DefaultHeatingInterval()
	r.PrintDensity = 10
	r.PrintBreakTime = 2

	// Clear volatile UDG and downloaded image RAM
	r.UserDefinedChars = make(map[byte]*UserDefinedChar)
	r.UserCharsEnabled = false
	r.DownloadedImage = nil
	r.QRSymbolBuffer = nil
}
