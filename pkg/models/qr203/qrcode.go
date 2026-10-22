package qr203

import (
	"thermalsim/pkg/printer"
)

// ProcessQRCommand handles the 2D QR Code command group GS ( k pL pH cn fn [params...]
// Returns rendered dot rows if fn == 81 (Print symbol data), otherwise nil.
func ProcessQRCommand(fn byte, payload []byte, reg *printer.Registers) [][]uint8 {
	switch fn {
	case 65: // 0x41: Select model
		if len(payload) >= 1 {
			reg.QRModel = int(payload[0] - '0')
		}
		return nil

	case 67: // 0x43: Set module size (1..16 dots)
		if len(payload) >= 1 {
			size := int(payload[0])
			if size >= 1 && size <= 16 {
				reg.QRModuleSize = size
			}
		}
		return nil

	case 69: // 0x45: Set error correction level
		if len(payload) >= 1 {
			reg.QRErrorLevel = int(payload[0])
		}
		return nil

	case 80: // 0x50: Store symbol data in buffer
		// Format: 0x31 0x50 0x30 [data...]
		if len(payload) >= 1 {
			// Strip leading '0' (0x30) or storage mode if present
			data := payload
			if len(data) > 0 && (data[0] == 0x30 || data[0] == 0x31) {
				data = data[1:]
			}
			reg.QRSymbolBuffer = make([]byte, len(data))
			copy(reg.QRSymbolBuffer, data)
		}
		return nil

	case 81: // 0x51: Print stored symbol data
		if len(reg.QRSymbolBuffer) == 0 {
			return nil
		}
		return RenderQRCodeMatrix(reg.QRSymbolBuffer, reg)

	default:
		return nil
	}
}

// RenderQRCodeMatrix builds a 2D QR matrix with finder patterns, timing lines, and encoded data.
func RenderQRCodeMatrix(data []byte, reg *printer.Registers) [][]uint8 {
	// Base size: 21x21 modules (Version 1) up to 29x29
	matrixDim := 25
	if len(data) > 20 {
		matrixDim = 29
	}
	if len(data) > 40 {
		matrixDim = 33
	}

	matrix := make([][]bool, matrixDim)
	for i := range matrix {
		matrix[i] = make([]bool, matrixDim)
	}

	// 1. Draw Finder Patterns (7x7 with 3x3 inner square) at top-left, top-right, bottom-left
	drawFinderPattern(matrix, 0, 0)
	drawFinderPattern(matrix, matrixDim-7, 0)
	drawFinderPattern(matrix, 0, matrixDim-7)

	// 2. Timing Patterns (row 6 and col 6)
	for i := 7; i < matrixDim-7; i++ {
		matrix[6][i] = (i % 2 == 0)
		matrix[i][6] = (i % 2 == 0)
	}

	// 3. Place Data modules with masking
	dataBitIdx := 0
	for y := 8; y < matrixDim-8; y++ {
		for x := 8; x < matrixDim-8; x++ {
			if dataBitIdx < len(data)*8 {
				byteIdx := dataBitIdx / 8
				bitIdx := 7 - (dataBitIdx % 8)
				bitVal := (data[byteIdx] & (1 << bitIdx)) != 0
				matrix[y][x] = bitVal
				dataBitIdx++
			} else {
				// Alternating filler pattern
				matrix[y][x] = ((x + y) % 2 == 0)
			}
		}
	}

	// 4. Scale modules into 384-dot lines based on QRModuleSize
	modSize := reg.QRModuleSize
	if modSize < 1 {
		modSize = 3
	}
	totalWidthDots := matrixDim * modSize
	leftMargin := reg.LeftMargin
	if leftMargin+totalWidthDots > 384 {
		leftMargin = 384 - totalWidthDots
		if leftMargin < 0 {
			leftMargin = 0
		}
	}

	totalRows := matrixDim * modSize
	renderedRows := make([][]uint8, totalRows)
	for r := 0; r < totalRows; r++ {
		renderedRows[r] = make([]uint8, 384)
		modY := r / modSize
		for col := 0; col < matrixDim; col++ {
			if matrix[modY][col] {
				for w := 0; w < modSize; w++ {
					xPos := leftMargin + col*modSize + w
					if xPos < 384 {
						renderedRows[r][xPos] = 1
					}
				}
			}
		}
	}

	return renderedRows
}

func drawFinderPattern(matrix [][]bool, startX, startY int) {
	for y := 0; y < 7; y++ {
		for x := 0; x < 7; x++ {
			// Outer 7x7 square
			if x == 0 || x == 6 || y == 0 || y == 6 {
				matrix[startY+y][startX+x] = true
			} else if x >= 2 && x <= 4 && y >= 2 && y <= 4 {
				// Inner 3x3 square
				matrix[startY+y][startX+x] = true
			} else {
				matrix[startY+y][startX+x] = false
			}
		}
	}
}
