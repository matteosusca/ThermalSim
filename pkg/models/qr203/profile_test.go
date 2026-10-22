package qr203

import (
	"testing"
	"thermalsim/pkg/printer"
)

func TestFontsAndMetrics(t *testing.T) {
	prof := NewQR203Profile(printer.FirmwareV269)

	// Font A glyph should be 12x24 = 288 bytes
	fontAGlyph := prof.RenderChar('A', printer.FontA, 0, 0)
	if len(fontAGlyph) != 12*24 {
		t.Fatalf("expected Font A glyph len %d, got %d", 12*24, len(fontAGlyph))
	}

	// Font B glyph should be 9x17 = 153 bytes
	fontBGlyph := prof.RenderChar('B', printer.FontB, 0, 0)
	if len(fontBGlyph) != 9*17 {
		t.Fatalf("expected Font B glyph len %d, got %d", 9*17, len(fontBGlyph))
	}
}

func TestInternationalCharset(t *testing.T) {
	// In Germany (ESC R 2), '@' should become '§'
	r := DecodeByteToRune('@', 0, 2)
	if r != '§' {
		t.Fatalf("expected '§' for German '@', got '%c'", r)
	}

	// In France (ESC R 1), '@' should become 'à'
	r = DecodeByteToRune('@', 0, 1)
	if r != 'à' {
		t.Fatalf("expected 'à' for French '@', got '%c'", r)
	}
}

func TestBarcodeGeneration(t *testing.T) {
	prof := NewQR203Profile(printer.FirmwareV269)
	reg := printer.NewRegisters(prof)

	// CODE39 barcode
	res := GenerateBarcode(4, []byte("TEST1234"), reg)
	if res == nil || len(res.Rows) < 50 {
		t.Fatalf("expected barcode rows >= 50, got %v", res)
	}
	if len(res.Rows[0]) != 384 {
		t.Fatalf("expected row width 384, got %d", len(res.Rows[0]))
	}

	// ITF odd-length rejection quirk
	resOdd := GenerateBarcode(5, []byte("123"), reg)
	if resOdd != nil {
		t.Fatal("expected odd-length ITF barcode to be rejected per hardware spec")
	}
}

func TestQRCodeGeneration(t *testing.T) {
	prof := NewQR203Profile(printer.FirmwareV269)
	reg := printer.NewRegisters(prof)

	// Store data (fn 80)
	ProcessQRCommand(80, []byte{0x30, 'H', 'E', 'L', 'L', 'O'}, reg)
	if string(reg.QRSymbolBuffer) != "HELLO" {
		t.Fatalf("expected 'HELLO' stored, got '%s'", string(reg.QRSymbolBuffer))
	}

	// Print symbol (fn 81)
	rows := ProcessQRCommand(81, nil, reg)
	if len(rows) == 0 {
		t.Fatal("expected non-empty rendered QR code rows")
	}
	if len(rows[0]) != 384 {
		t.Fatalf("expected row width 384, got %d", len(rows[0]))
	}
}

func TestSelfTestGeneration(t *testing.T) {
	prof := NewQR203Profile(printer.FirmwareV269)
	reg := printer.NewRegisters(prof)

	rows := GenerateSelfTestPage(prof, reg)
	if len(rows) < 300 {
		t.Fatalf("expected self-test rows >= 300, got %d", len(rows))
	}
}
