package printer

// FirmwareRevision represents the firmware generation of the printer.
type FirmwareRevision string

const (
	FirmwareV216 FirmwareRevision = "2.16"
	FirmwareV269 FirmwareRevision = "2.69"
)

// FontType identifies the active character font matrix.
type FontType int

const (
	FontA FontType = iota // 12x24 dots (32 cols per 384-dot line)
	FontB                 // 9x17 dots (42 cols per 384-dot line)
	FontKanji             // 24x24 dots (16 cols per 384-dot line)
)

// PaperStatus represents the state of the paper roll sensor.
type PaperStatus int

const (
	PaperAdequate PaperStatus = iota
	PaperNearEnd
	PaperOut
)

// Profile defines the hardware parameters and capabilities of a thermal printer.
type Profile interface {
	Name() string
	FirmwareVersion() FirmwareRevision
	DotsPerLine() int          // e.g. 384 dots for 58mm QR203
	BytesPerLine() int         // e.g. 48 bytes
	DefaultBaudRate() int      // e.g. 19200 bps
	ReceiveBufferSize() int    // e.g. 4096 bytes (v2.69) or 256 bytes (v2.16 legacy)
	DefaultLineSpacing() uint8 // e.g. 30 dots (v2.69) or 32 dots (v2.16)
	MaxHeatingDots() uint8     // default n1
	DefaultHeatingTime() uint8 // default n2
	DefaultHeatingInterval() uint8 // default n3

	// Character rendering lookup
	RenderChar(code byte, font FontType, codePage int, intlSet int) []byte
	RenderKanji(c1, c2 byte) []byte

	// Supported capabilities
	SupportsQRCode() bool
	SupportsBarcode(system int) bool
}
