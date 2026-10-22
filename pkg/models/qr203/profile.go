package qr203

import (
	"thermalsim/pkg/printer"
)

// QR203Profile implements printer.Profile for the QR203 / CSN-A2 thermal printer.
type QR203Profile struct {
	version printer.FirmwareRevision
}

// NewQR203Profile creates a profile with the specified firmware generation.
func NewQR203Profile(version printer.FirmwareRevision) *QR203Profile {
	if version == "" {
		version = printer.FirmwareV269
	}
	return &QR203Profile{version: version}
}

func (p *QR203Profile) Name() string {
	return "QR203"
}

func (p *QR203Profile) FirmwareVersion() printer.FirmwareRevision {
	return p.version
}

func (p *QR203Profile) DotsPerLine() int {
	return 384 // 48 mm at 8 dots/mm
}

func (p *QR203Profile) BytesPerLine() int {
	return 48 // 384 / 8
}

func (p *QR203Profile) DefaultBaudRate() int {
	return 19200
}

func (p *QR203Profile) ReceiveBufferSize() int {
	if p.version == printer.FirmwareV216 {
		return 256 // Strict legacy clone SRAM limit
	}
	return 4096 // Standard v2.64-v2.69 SRAM limit
}

func (p *QR203Profile) DefaultLineSpacing() uint8 {
	if p.version == printer.FirmwareV216 {
		return 32
	}
	return 30
}

func (p *QR203Profile) MaxHeatingDots() uint8 {
	return 7 // 8 * (7 + 1) = 64 dots
}

func (p *QR203Profile) DefaultHeatingTime() uint8 {
	return 80 // 800 us
}

func (p *QR203Profile) DefaultHeatingInterval() uint8 {
	return 2 // 20 us
}

func (p *QR203Profile) RenderChar(code byte, font printer.FontType, codePage int, intlSet int) []byte {
	// Translate byte via CodePage and IntlCharset
	r := DecodeByteToRune(code, codePage, intlSet)
	resolvedCode := byte(r)
	if r > 0x7E && r <= 0xFF {
		resolvedCode = byte(r)
	}

	switch font {
	case printer.FontB:
		return RenderGlyphFontB(resolvedCode)
	case printer.FontKanji:
		return RenderGlyphKanji(code, 0)
	default:
		return RenderGlyphFontA(resolvedCode)
	}
}

func (p *QR203Profile) RenderKanji(c1, c2 byte) []byte {
	return RenderGlyphKanji(c1, c2)
}

func (p *QR203Profile) GenerateBarcode(m int, data []byte, reg *printer.Registers) *printer.BarcodeResult {
	return GenerateBarcode(m, data, reg)
}

func (p *QR203Profile) ProcessQRCommand(fn byte, payload []byte, reg *printer.Registers) [][]uint8 {
	return ProcessQRCommand(fn, payload, reg)
}

func (p *QR203Profile) GenerateSelfTestPage(reg *printer.Registers) [][]uint8 {
	return GenerateSelfTestPage(p, reg)
}

func (p *QR203Profile) SupportsQRCode() bool {
	// QR codes are supported natively on v2.64+ firmware
	return p.version != printer.FirmwareV216
}

func (p *QR203Profile) SupportsBarcode(system int) bool {
	// Barcode system range
	if p.version == printer.FirmwareV216 {
		return system >= 0 && system <= 10
	}
	return (system >= 0 && system <= 10) || (system >= 65 && system <= 75)
}
