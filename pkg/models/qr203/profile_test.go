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

	// Verify native 12x24 CGROM glyph 'A' has serifs at baseline row 20 (0xF0F = ####....####)
	if fontA12x24['A'-0x20][20] != 0xF0F {
		t.Fatalf("expected native ASC24 baseline serif 0xF0F for 'A', got 0x%03X", fontA12x24['A'-0x20][20])
	}

	// Verify native 9x17 CGROM glyph 'A' has serifs at baseline row 14 (0x1C7 = ###...###)
	if fontB9x17['A'-0x20][14] != 0x1C7 {
		t.Fatalf("expected native ASC17 baseline serif 0x1C7 for 'A', got 0x%03X", fontB9x17['A'-0x20][14])
	}

	// Verify extended CP437, CP858 Euro, and International characters render non-empty glyphs
	extGlyphs := [][]byte{
		prof.RenderChar(0x82, printer.FontA, 0, 0),  // CP437 'é'
		prof.RenderChar(0x9C, printer.FontA, 0, 0),  // CP437 '£'
		prof.RenderChar(0xDB, printer.FontA, 0, 0),  // CP437 '█'
		prof.RenderChar(0xC4, printer.FontA, 0, 0),  // CP437 '─'
		prof.RenderChar(0xD5, printer.FontA, 19, 0), // CP858 '€'
		prof.RenderChar('@', printer.FontA, 0, 1),   // France 'à'
		prof.RenderChar('@', printer.FontB, 0, 2),   // Germany '§' in Font B
	}
	for i, g := range extGlyphs {
		dots := 0
		for _, v := range g {
			if v > 0 {
				dots++
			}
		}
		if dots == 0 {
			t.Fatalf("expected non-empty bitmap for extended glyph case %d", i)
		}
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

	// 1. CODE39 barcode
	res := GenerateBarcode(4, []byte("TEST1234"), reg)
	if res == nil || len(res.Rows) < 50 {
		t.Fatalf("expected barcode rows >= 50, got %v", res)
	}
	if len(res.Rows[0]) != 384 {
		t.Fatalf("expected row width 384, got %d", len(res.Rows[0]))
	}

	// 2. ITF odd-length rejection quirk (CSN-A2 Manual p. 28)
	resOdd := GenerateBarcode(5, []byte("123"), reg)
	if resOdd != nil {
		t.Fatal("expected odd-length ITF barcode to be rejected per hardware spec")
	}

	// 3. ITF even-length valid encoding (Start 1010, Stop 1101)
	resITF := GenerateBarcode(5, []byte("1234"), reg)
	if resITF == nil || len(resITF.Rows) == 0 {
		t.Fatal("expected valid even-length ITF barcode to generate successfully")
	}

	// 4. Code 128 (ISO/IEC 15417)
	res128 := GenerateBarcode(73, []byte("Code128"), reg)
	if res128 == nil || len(res128.Rows) == 0 {
		t.Fatal("expected valid Code 128 barcode")
	}

	// 5. EAN-13 (95 modules: 3 guard + 42 left + 5 center + 42 right + 3 guard)
	modulesEAN := encodeEAN13("5901234123457")
	if len(modulesEAN) != 95 {
		t.Fatalf("expected exactly 95 modules for EAN-13, got %d", len(modulesEAN))
	}
	// Check standard left and right guard bars: 101
	if !modulesEAN[0] || modulesEAN[1] || !modulesEAN[2] {
		t.Fatalf("expected left guard 101 for EAN-13")
	}
	if !modulesEAN[92] || modulesEAN[93] || !modulesEAN[94] {
		t.Fatalf("expected right guard 101 for EAN-13")
	}
}

func TestQRCodeReedSolomonAndMatrix(t *testing.T) {
	prof := NewQR203Profile(printer.FirmwareV269)
	reg := printer.NewRegisters(prof)

	// 1. Galois Field arithmetic
	initGF()
	if gfMul(0, 15) != 0 || gfMul(15, 0) != 0 {
		t.Fatal("gfMul by 0 must return 0")
	}
	if gfMul(1, 42) != 42 || gfMul(42, 1) != 42 {
		t.Fatal("gfMul by 1 must return identical value")
	}
	// In GF(256) with modulo 0x11D: 2 * 128 = 256 ^ 285 = 29
	if gfMul(2, 128) != 29 {
		t.Fatalf("expected gfMul(2, 128) == 29, got %d", gfMul(2, 128))
	}

	// 2. Reed-Solomon ECC generator
	data := []uint8{0x10, 0x20, 0x0C, 0x56, 0x61, 0x80, 0xEC, 0x11, 0xEC}
	ecc := rsCalculateECC(data, 7)
	if len(ecc) != 7 {
		t.Fatalf("expected 7 ECC codewords, got %d", len(ecc))
	}

	// 3. QR Matrix rendering
	reg.QRModuleSize = 3
	matrix := RenderQRCodeMatrix([]byte("THERMALSIM"), reg)
	if len(matrix) == 0 {
		t.Fatal("expected non-empty rendered QR code matrix")
	}
	// Row width must match printer line buffer width
	if len(matrix[0]) != 384 {
		t.Fatalf("expected row width 384, got %d", len(matrix[0]))
	}

	// 4. ProcessQRCommand fn 80 and 81
	ProcessQRCommand(80, []byte{0x30, 'P', 'A', 'S', 'S'}, reg)
	if string(reg.QRSymbolBuffer) != "PASS" {
		t.Fatalf("expected 'PASS' in QRSymbolBuffer, got %q", string(reg.QRSymbolBuffer))
	}
	qrRows := ProcessQRCommand(81, nil, reg)
	if len(qrRows) == 0 {
		t.Fatal("expected rendered rows from fn 81")
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
