package qr203

import (
	"thermalsim/pkg/printer"
)

// BarcodePattern defines a 1D barcode bar/space module bitmask.
type BarcodeResult struct {
	Rows [][]uint8 // Each row is 384 dots (1 = black, 0 = white)
	HRI  string    // Human readable text
}

// GenerateBarcode renders a 1D barcode according to ESC/POS GS k specifications.
func GenerateBarcode(m int, data []byte, reg *printer.Registers) *BarcodeResult {
	// Normalize m to 0..8
	system := m
	if system >= 65 {
		system -= 65
	}

	hriText := string(data)
	var modules []bool

	switch system {
	case 4: // CODE39
		modules = encodeCode39(string(data))
	case 5: // ITF (Interleaved 2 of 5)
		if len(data)%2 != 0 {
			// Odd length: physical printer rejects and only feeds paper!
			return nil
		}
		modules = encodeITF(string(data))
	case 8: // CODE128
		modules = encodeCode128(data)
	default:
		// Default fallback generic barcode encoding
		modules = encodeGenericBarcode(data)
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

	res := &BarcodeResult{
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

// Simple standard CODE39 encoder (start/stop '*' included)
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
		// 9 elements: 5 bars, 4 spaces. '1' = wide (3), '0' = narrow (1)
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
		// Inter-character space
		modules = append(modules, false)
	}
	return modules
}

func encodeITF(text string) []bool {
	var modules []bool
	// Simple interleaved 2 of 5 representation
	for _, c := range text {
		val := int(c - '0')
		if val < 0 || val > 9 {
			val = 0
		}
		for i := 0; i < 5; i++ {
			modules = append(modules, (val&(1<<i)) != 0)
			modules = append(modules, false)
		}
	}
	return modules
}

func encodeCode128(data []byte) []bool {
	// Standard start bar + data bars + stop bar
	var modules []bool
	modules = append(modules, true, true, false, true, false, false) // Start
	for _, b := range data {
		for i := 0; i < 8; i++ {
			modules = append(modules, (b&(1<<i)) != 0)
		}
		modules = append(modules, false)
	}
	modules = append(modules, true, true, false, false, true, true, true) // Stop
	return modules
}

func encodeGenericBarcode(data []byte) []bool {
	var modules []bool
	modules = append(modules, true, false, true) // Guard
	for _, b := range data {
		for i := 0; i < 7; i++ {
			modules = append(modules, (b&(1<<i)) != 0)
		}
		modules = append(modules, false)
	}
	modules = append(modules, true, false, true) // Guard
	return modules
}
