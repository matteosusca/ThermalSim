package qr203

import (
	"thermalsim/pkg/printer"
)

// GenerateBarcode renders a 1D barcode according to ESC/POS GS k specifications.
func GenerateBarcode(m int, data []byte, reg *printer.Registers) *printer.BarcodeResult {
	// Normalize m: System 0..8 (Format 1) or System 65..73 (Format 2)
	system := m
	if system >= 65 {
		system -= 65
	}

	hriText := string(data)
	var modules []bool

	switch system {
	case 0: // UPC-A
		modules = encodeUPCA(string(data))
	case 2: // EAN13
		modules = encodeEAN13(string(data))
	case 3: // EAN8
		modules = encodeEAN8(string(data))
	case 4: // CODE39
		modules = encodeCode39(string(data))
	case 5: // ITF (Interleaved 2 of 5)
		if len(data)%2 != 0 {
			// Odd length: physical QR203 rejects and only feeds paper per spec
			return nil
		}
		modules = encodeITF(string(data))
	case 8: // CODE128
		modules = encodeCode128(data)
	default:
		// Default fallback to Code 39 or Code 128
		modules = encodeCode128(data)
	}

	if len(modules) == 0 {
		return nil
	}

	// Module width (2..6 dots per module, default 3)
	modWidth := reg.BarcodeWidth
	if modWidth < 2 {
		modWidth = 2
	} else if modWidth > 6 {
		modWidth = 6
	}

	// Calculate total width in dots
	totalBarcodeDots := len(modules) * modWidth
	leftMargin := reg.BarcodeLeftSpace + reg.LeftMargin
	if leftMargin+totalBarcodeDots > 384 {
		leftMargin = 384 - totalBarcodeDots
		if leftMargin < 0 {
			leftMargin = 0
		}
	}

	barcodeHeight := reg.BarcodeHeight
	if barcodeHeight <= 0 {
		barcodeHeight = 50
	}

	res := &printer.BarcodeResult{
		Rows: make([][]uint8, 0, barcodeHeight+30),
		HRI:  hriText,
	}

	// 1. If HRI is above (1 or 3), render HRI text line first
	if reg.BarcodeHRIPosition == 1 || reg.BarcodeHRIPosition == 3 {
		hriRows := renderHRILine(hriText, leftMargin, totalBarcodeDots)
		res.Rows = append(res.Rows, hriRows...)
	}

	// 2. Render barcode body rows
	barcodeRow := make([]uint8, 384)
	for i, isBar := range modules {
		if isBar {
			for w := 0; w < modWidth; w++ {
				pos := leftMargin + i*modWidth + w
				if pos < 384 {
					barcodeRow[pos] = 1
				}
			}
		}
	}

	for r := 0; r < barcodeHeight; r++ {
		rowCopy := make([]uint8, 384)
		copy(rowCopy, barcodeRow)
		res.Rows = append(res.Rows, rowCopy)
	}

	// 3. If HRI is below (2 or 3), render HRI text line below
	if reg.BarcodeHRIPosition == 2 || reg.BarcodeHRIPosition == 3 {
		hriRows := renderHRILine(hriText, leftMargin, totalBarcodeDots)
		res.Rows = append(res.Rows, hriRows...)
	}

	return res
}

func renderHRILine(text string, leftMargin, barcodeWidth int) [][]uint8 {
	textWidth := len(text) * 12
	startX := leftMargin + (barcodeWidth-textWidth)/2
	if startX < 0 {
		startX = 0
	}

	rows := make([][]uint8, 24)
	for i := range rows {
		rows[i] = make([]uint8, 384)
	}

	for charIdx, c := range text {
		glyph := RenderGlyphFontA(byte(c))
		charLeft := startX + charIdx*12
		if charLeft+12 > 384 {
			break
		}
		for gy := 0; gy < 24; gy++ {
			for gx := 0; gx < 12; gx++ {
				if glyph[gy*12+gx] > 0 {
					rows[gy][charLeft+gx] = 1
				}
			}
		}
	}
	return rows
}

// --------------------------------------------------------------------------
// 1. CODE 39 Standard Encoder
// --------------------------------------------------------------------------

var code39Table = map[rune]string{
	'0': "000110100", '1': "100100001", '2': "001100001", '3': "101100000",
	'4': "000110001", '5': "100110000", '6': "001110000", '7': "000100101",
	'8': "100100100", '9': "001100100", 'A': "100001001", 'B': "001001001",
	'C': "101001000", 'D': "000011001", 'E': "100011000", 'F': "001011000",
	'G': "000001101", 'H': "100001100", 'I': "001001100", 'J': "000011100",
	'K': "100000011", 'L': "001000011", 'M': "101000010", 'N': "000010011",
	'O': "100010010", 'P': "001010010", 'Q': "000000111", 'R': "100000110",
	'S': "001000110", 'T': "000010110", 'U': "110000001", 'V': "011000001",
	'W': "111000000", 'X': "010010001", 'Y': "110010000", 'Z': "011010000",
	'-': "010000101", '.': "110000100", ' ': "011000100", '$': "010101000",
	'/': "010100010", '+': "010001010", '%': "000101010", '*': "010010100",
}

func encodeCode39(text string) []bool {
	str := "*" + text + "*"
	var modules []bool
	for _, ch := range str {
		pat, ok := code39Table[ch]
		if !ok {
			pat = code39Table['-']
		}
		for i, b := range pat {
			isBar := (i % 2 == 0)
			width := 1
			if b == '1' {
				width = 2
			}
			for w := 0; w < width; w++ {
				modules = append(modules, isBar)
			}
		}
		// Inter-character narrow space
		modules = append(modules, false)
	}
	return modules
}

// --------------------------------------------------------------------------
// 2. Interleaved 2 of 5 (ITF) Standard Encoder
// --------------------------------------------------------------------------

// itfWeights: 5 binary elements per digit (0..9) with exactly two wide ('1') elements.
var itfWeights = [10][5]bool{
	{false, false, true, true, false}, // 0
	{true, false, false, false, true}, // 1
	{false, true, false, false, true}, // 2
	{true, true, false, false, false}, // 3
	{false, false, true, false, true}, // 4
	{true, false, true, false, false}, // 5
	{false, true, true, false, false}, // 6
	{false, false, false, true, true}, // 7
	{true, false, false, true, false}, // 8
	{false, true, false, true, false}, // 9
}

func encodeITF(text string) []bool {
	var modules []bool

	// Start pattern: narrow bar, narrow space, narrow bar, narrow space (1010)
	modules = append(modules, true, false, true, false)

	// Interleave pairs of digits
	for p := 0; p < len(text); p += 2 {
		d1 := int(text[p] - '0')
		d2 := int(text[p+1] - '0')
		if d1 < 0 || d1 > 9 {
			d1 = 0
		}
		if d2 < 0 || d2 > 9 {
			d2 = 0
		}

		w1 := itfWeights[d1] // Bars
		w2 := itfWeights[d2] // Spaces

		for i := 0; i < 5; i++ {
			// Bar width (wide=2, narrow=1)
			barWidth := 1
			if w1[i] {
				barWidth = 2
			}
			for w := 0; w < barWidth; w++ {
				modules = append(modules, true)
			}

			// Space width (wide=2, narrow=1)
			spaceWidth := 1
			if w2[i] {
				spaceWidth = 2
			}
			for w := 0; w < spaceWidth; w++ {
				modules = append(modules, false)
			}
		}
	}

	// Stop pattern: wide bar, narrow space, narrow bar (1101)
	modules = append(modules, true, true, false, true)
	return modules
}

// --------------------------------------------------------------------------
// 3. CODE 128 Standard Encoder (ISO/IEC 15417)
// --------------------------------------------------------------------------

// code128Widths contains the 6 element widths (b1, s1, b2, s2, b3, s3) totaling 11 modules for codes 0..105.
var code128Widths = [106][6]uint8{
	{2, 1, 2, 2, 2, 2}, {2, 2, 2, 1, 2, 2}, {2, 2, 2, 2, 2, 1}, {1, 2, 1, 2, 2, 3},
	{1, 2, 1, 3, 2, 2}, {1, 3, 1, 2, 2, 2}, {1, 2, 2, 2, 1, 3}, {1, 2, 2, 3, 1, 2},
	{1, 3, 2, 2, 1, 2}, {2, 2, 1, 2, 1, 3}, {2, 2, 1, 3, 1, 2}, {2, 3, 1, 2, 1, 2},
	{1, 1, 2, 2, 3, 2}, {1, 2, 2, 1, 3, 2}, {1, 2, 2, 2, 3, 1}, {1, 1, 3, 2, 2, 2},
	{1, 2, 3, 1, 2, 2}, {1, 2, 3, 2, 2, 1}, {2, 2, 3, 2, 1, 1}, {2, 2, 1, 1, 3, 2},
	{2, 2, 1, 2, 3, 1}, {2, 1, 3, 2, 1, 2}, {2, 2, 3, 1, 1, 2}, {3, 1, 2, 1, 3, 1},
	{3, 1, 1, 2, 2, 2}, {3, 2, 1, 1, 2, 2}, {3, 2, 1, 2, 2, 1}, {3, 1, 2, 2, 1, 2},
	{3, 2, 2, 1, 1, 2}, {3, 2, 2, 2, 1, 1}, {2, 1, 2, 1, 2, 3}, {2, 1, 2, 3, 2, 1},
	{2, 3, 2, 1, 2, 1}, {1, 1, 1, 3, 2, 3}, {1, 3, 1, 1, 2, 3}, {1, 3, 1, 3, 2, 1},
	{1, 1, 2, 3, 1, 3}, {1, 3, 2, 1, 1, 3}, {1, 3, 2, 3, 1, 1}, {2, 1, 1, 3, 1, 3},
	{2, 3, 1, 1, 1, 3}, {2, 3, 1, 3, 1, 1}, {1, 1, 2, 1, 3, 3}, {1, 1, 2, 3, 3, 1},
	{1, 3, 2, 1, 3, 1}, {1, 1, 3, 1, 2, 3}, {1, 1, 3, 3, 2, 1}, {1, 3, 3, 1, 2, 1},
	{3, 1, 3, 1, 2, 1}, {2, 1, 1, 3, 3, 1}, {2, 3, 1, 1, 3, 1}, {2, 1, 3, 1, 1, 3},
	{2, 1, 3, 3, 1, 1}, {2, 1, 3, 1, 3, 1}, {3, 1, 1, 1, 2, 3}, {3, 1, 1, 3, 2, 1},
	{3, 3, 1, 1, 2, 1}, {3, 1, 2, 1, 1, 3}, {3, 1, 2, 3, 1, 1}, {3, 3, 2, 1, 1, 1},
	{3, 1, 4, 1, 1, 1}, {2, 2, 1, 4, 1, 1}, {4, 3, 1, 1, 1, 1}, {1, 1, 1, 2, 2, 4},
	{1, 1, 1, 4, 2, 2}, {1, 2, 1, 1, 2, 4}, {1, 2, 1, 4, 2, 1}, {1, 4, 1, 1, 2, 2},
	{1, 4, 1, 2, 2, 1}, {1, 1, 2, 2, 1, 4}, {1, 1, 2, 4, 1, 2}, {1, 2, 2, 1, 1, 4},
	{1, 2, 2, 4, 1, 1}, {1, 4, 2, 1, 1, 2}, {1, 4, 2, 2, 1, 1}, {2, 4, 1, 2, 1, 1},
	{2, 2, 1, 1, 1, 4}, {4, 1, 3, 1, 1, 1}, {2, 4, 1, 1, 1, 2}, {1, 3, 4, 1, 1, 1},
	{1, 1, 1, 2, 4, 2}, {1, 2, 1, 1, 4, 2}, {1, 2, 1, 2, 4, 1}, {1, 1, 4, 2, 1, 2},
	{1, 2, 4, 1, 1, 2}, {1, 2, 4, 2, 1, 1}, {4, 1, 1, 2, 1, 2}, {4, 2, 1, 1, 1, 2},
	{4, 2, 1, 2, 1, 1}, {2, 1, 2, 1, 4, 1}, {2, 1, 4, 1, 2, 1}, {4, 1, 2, 1, 2, 1},
	{1, 1, 1, 1, 4, 3}, {1, 1, 1, 3, 4, 1}, {1, 3, 1, 1, 4, 1}, {1, 1, 4, 1, 1, 3},
	{1, 1, 4, 3, 1, 1}, {4, 1, 1, 1, 1, 3}, {4, 1, 1, 3, 1, 1}, {1, 1, 3, 1, 4, 1},
	{1, 1, 4, 1, 3, 1}, {3, 1, 1, 1, 4, 1}, {4, 1, 1, 1, 3, 1}, {2, 1, 1, 4, 1, 2},
	{2, 1, 1, 2, 1, 4}, {2, 1, 1, 2, 3, 2},
}

// Stop pattern: 106 has 7 elements (13 modules): bar 2, space 3, bar 3, space 1, bar 1, space 1, bar 2.
var code128Stop = [7]uint8{2, 3, 3, 1, 1, 1, 2}

func encodeCode128(data []byte) []bool {
	if len(data) == 0 {
		return nil
	}

	// Use Code 128 Subset B (Start B = value 104)
	codes := make([]int, 0, len(data)+3)
	codes = append(codes, 104) // Start B

	checksum := 104
	for idx, b := range data {
		val := int(b) - 32
		if val < 0 || val > 105 {
			val = 0
		}
		codes = append(codes, val)
		checksum += (idx + 1) * val
	}

	checkVal := checksum % 103
	codes = append(codes, checkVal)

	var modules []bool
	// Emit symbols
	for _, c := range codes {
		w := code128Widths[c]
		for i := 0; i < 6; i++ {
			isBar := (i % 2 == 0)
			for m := uint8(0); m < w[i]; m++ {
				modules = append(modules, isBar)
			}
		}
	}

	// Emit Stop pattern (106)
	for i := 0; i < 7; i++ {
		isBar := (i % 2 == 0)
		for m := uint8(0); m < code128Stop[i]; m++ {
			modules = append(modules, isBar)
		}
	}

	return modules
}

// --------------------------------------------------------------------------
// 4. EAN-13 / UPC-A Standard Encoder
// --------------------------------------------------------------------------

// EAN-13 Digit patterns: L (Left odd parity), G (Left even parity), R (Right parity)
var eanL = [10]string{"0001101", "0011001", "0010011", "0111101", "0100011", "0110001", "0101111", "0111011", "0110111", "0001011"}
var eanG = [10]string{"0100111", "0110011", "0011011", "0100001", "0011101", "0111001", "0000101", "0010001", "0001001", "0010111"}
var eanR = [10]string{"1110010", "1100110", "1101100", "1000010", "1011100", "1001110", "1010000", "1000100", "1001000", "1110100"}

// EAN-13 Parity structures for first digit 0..9
var eanParity = [10]string{
	"LLLLLL", "LLGLGG", "LLGGLG", "LLGGGL", "LGLLGG",
	"LGGLLG", "LGGGLL", "LGLGLG", "LGLGGL", "LGGLGL",
}

func encodeEAN13(text string) []bool {
	digits := make([]int, 0, 13)
	for _, c := range text {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 12 {
		return nil
	}
	for len(digits) < 13 {
		// Calculate checksum digit
		sum := 0
		for i := 0; i < 12; i++ {
			if i%2 == 0 {
				sum += digits[i]
			} else {
				sum += digits[i] * 3
			}
		}
		check := (10 - (sum % 10)) % 10
		digits = append(digits, check)
	}

	var modules []bool
	// Left Guard: 101
	appendPattern(&modules, "101")

	first := digits[0]
	par := eanParity[first]

	// 6 Left digits
	for i := 1; i <= 6; i++ {
		d := digits[i]
		if par[i-1] == 'L' {
			appendPattern(&modules, eanL[d])
		} else {
			appendPattern(&modules, eanG[d])
		}
	}

	// Center Guard: 01010
	appendPattern(&modules, "01010")

	// 6 Right digits (R parity)
	for i := 7; i <= 12; i++ {
		d := digits[i]
		appendPattern(&modules, eanR[d])
	}

	// Right Guard: 101
	appendPattern(&modules, "101")
	return modules
}

func encodeUPCA(text string) []bool {
	// UPC-A is EAN-13 with leading '0'
	return encodeEAN13("0" + text)
}

func encodeEAN8(text string) []bool {
	digits := make([]int, 0, 8)
	for _, c := range text {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 7 {
		return nil
	}
	for len(digits) < 8 {
		sum := 0
		for i := 0; i < 7; i++ {
			if i%2 == 0 {
				sum += digits[i] * 3
			} else {
				sum += digits[i]
			}
		}
		check := (10 - (sum % 10)) % 10
		digits = append(digits, check)
	}

	var modules []bool
	appendPattern(&modules, "101") // Left Guard
	for i := 0; i < 4; i++ {
		appendPattern(&modules, eanL[digits[i]])
	}
	appendPattern(&modules, "01010") // Center Guard
	for i := 4; i < 8; i++ {
		appendPattern(&modules, eanR[digits[i]])
	}
	appendPattern(&modules, "101") // Right Guard
	return modules
}

func appendPattern(modules *[]bool, pattern string) {
	for _, ch := range pattern {
		*modules = append(*modules, ch == '1')
	}
}
