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

	case 69: // 0x45: Set error correction level (48=L, 49=M, 50=Q, 51=H)
		if len(payload) >= 1 {
			reg.QRErrorLevel = int(payload[0])
		}
		return nil

	case 80: // 0x50: Store symbol data in buffer
		// Format: 0x31 0x50 0x30 [data...]
		if len(payload) >= 1 {
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

// --------------------------------------------------------------------------
// Standard ISO/IEC 18004 QR Code Matrix Generator (Pure Go, Zero Dependencies)
// --------------------------------------------------------------------------

// Galois Field GF(256) tables for Reed-Solomon Error Correction
var gfExp [512]uint8
var gfLog [256]uint8
var gfInitialized bool

func initGF() {
	if gfInitialized {
		return
	}
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = uint8(x)
		gfExp[i+255] = uint8(x)
		gfLog[x] = uint8(i)
		x <<= 1
		if x >= 256 {
			x ^= 0x11D // Primitive polynomial x^8 + x^4 + x^3 + x^2 + 1
		}
	}
	gfInitialized = true
}

func gfMul(a, b uint8) uint8 {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func rsGeneratorPoly(degree int) []uint8 {
	initGF()
	g := []uint8{1}
	for i := 0; i < degree; i++ {
		next := make([]uint8, len(g)+1)
		alpha := gfExp[i]
		for j, c := range g {
			next[j] ^= c
			next[j+1] ^= gfMul(c, alpha)
		}
		g = next
	}
	return g
}

func rsCalculateECC(data []uint8, ecCount int) []uint8 {
	poly := rsGeneratorPoly(ecCount)
	rem := make([]uint8, len(data)+ecCount)
	copy(rem, data)
	for i := 0; i < len(data); i++ {
		lead := rem[i]
		if lead != 0 {
			for j, c := range poly {
				rem[i+j] ^= gfMul(lead, c)
			}
		}
	}
	return rem[len(data):]
}

type qrVersionSpec struct {
	version   int
	dimension int
	dataBytes int
	ecBytes   int
	alignPos  int // 0 if none
	formatBits uint16 // Formatted bits with BCH mask
}

// Precomputed version parameters for Level M (default) and Level L
func getVersionSpec(dataLen int, ecLevel int) qrVersionSpec {
	// Level M (49) vs Level L (48)
	if ecLevel == 48 { // Level L
		if dataLen <= 17 {
			return qrVersionSpec{1, 21, 19, 7, 0, 0x77C4}
		} else if dataLen <= 32 {
			return qrVersionSpec{2, 25, 34, 10, 18, 0x77C4}
		} else if dataLen <= 53 {
			return qrVersionSpec{3, 29, 55, 15, 22, 0x77C4}
		} else {
			return qrVersionSpec{4, 33, 78, 20, 26, 0x77C4}
		}
	}
	// Default: Level M
	if dataLen <= 14 {
		return qrVersionSpec{1, 21, 16, 10, 0, 0x5412}
	} else if dataLen <= 26 {
		return qrVersionSpec{2, 25, 28, 16, 18, 0x5412}
	} else if dataLen <= 42 {
		return qrVersionSpec{3, 29, 44, 26, 22, 0x5412}
	} else {
		return qrVersionSpec{4, 33, 64, 36, 26, 0x5412}
	}
}

// RenderQRCodeMatrix builds a valid, scannable ISO/IEC 18004 2D QR matrix.
func RenderQRCodeMatrix(data []byte, reg *printer.Registers) [][]uint8 {
	initGF()

	spec := getVersionSpec(len(data), reg.QRErrorLevel)
	dim := spec.dimension

	// 1. Encode Data Codewords (Byte Mode)
	var bitBuf []bool
	// Mode Indicator: Byte (0100)
	appendBits(&bitBuf, 4, 4)
	// Character Count Indicator (8 bits for Vers 1..9 in byte mode)
	appendBits(&bitBuf, uint32(len(data)), 8)
	// Payload
	for _, b := range data {
		appendBits(&bitBuf, uint32(b), 8)
	}
	// Terminator (up to 4 zeroes)
	termLen := spec.dataBytes*8 - len(bitBuf)
	if termLen > 4 {
		termLen = 4
	}
	if termLen > 0 {
		appendBits(&bitBuf, 0, termLen)
	}
	// Pad to byte boundary
	for len(bitBuf)%8 != 0 {
		bitBuf = append(bitBuf, false)
	}
	// Convert to bytes
	dataCodewords := make([]uint8, len(bitBuf)/8)
	for i := range dataCodewords {
		var val uint8
		for b := 0; b < 8; b++ {
			if bitBuf[i*8+b] {
				val |= 1 << (7 - b)
			}
		}
		dataCodewords[i] = val
	}
	// Pad bytes (0xEC, 0x11)
	padBytes := []uint8{0xEC, 0x11}
	pIdx := 0
	for len(dataCodewords) < spec.dataBytes {
		dataCodewords = append(dataCodewords, padBytes[pIdx%2])
		pIdx++
	}

	// 2. Generate Reed-Solomon Error Correction Codewords
	ecCodewords := rsCalculateECC(dataCodewords, spec.ecBytes)
	allCodewords := append(dataCodewords, ecCodewords...)

	// 3. Matrix allocation and function pattern reservation
	matrix := make([][]bool, dim)
	reserved := make([][]bool, dim)
	for i := range matrix {
		matrix[i] = make([]bool, dim)
		reserved[i] = make([]bool, dim)
	}

	// 3a. Finder patterns & separators (top-left, top-right, bottom-left)
	drawFinderWithSeparator(matrix, reserved, 0, 0, dim)
	drawFinderWithSeparator(matrix, reserved, dim-7, 0, dim)
	drawFinderWithSeparator(matrix, reserved, 0, dim-7, dim)

	// 3b. Timing patterns (row 6 and col 6)
	for i := 8; i < dim-8; i++ {
		matrix[6][i] = (i%2 == 0)
		reserved[6][i] = true
		matrix[i][6] = (i%2 == 0)
		reserved[i][6] = true
	}

	// 3c. Alignment pattern (for Version >= 2)
	if spec.alignPos > 0 {
		drawAlignmentPattern(matrix, reserved, spec.alignPos, spec.alignPos)
	}

	// 3d. Reserve format information areas
	for i := 0; i < 9; i++ {
		reserved[8][i] = true
		reserved[i][8] = true
	}
	for i := dim - 8; i < dim; i++ {
		reserved[8][i] = true
		reserved[i][8] = true
	}
	// Dark module
	matrix[dim-8][8] = true
	reserved[dim-8][8] = true

	// 4. Place Data Bits in 2-column zigzag pairs
	dataBits := make([]bool, 0, len(allCodewords)*8)
	for _, cw := range allCodewords {
		for b := 7; b >= 0; b-- {
			dataBits = append(dataBits, (cw&(1<<b)) != 0)
		}
	}

	bitIdx := 0
	upwards := true
	for right := dim - 1; right > 0; right -= 2 {
		if right == 6 {
			right-- // Skip vertical timing column
		}
		for row := 0; row < dim; row++ {
			r := row
			if upwards {
				r = dim - 1 - row
			}
			for colOffset := 0; colOffset < 2; colOffset++ {
				c := right - colOffset
				if !reserved[r][c] {
					bitVal := false
					if bitIdx < len(dataBits) {
						bitVal = dataBits[bitIdx]
						bitIdx++
					}
					// Apply standard Mask 000: (r + c) % 2 == 0
					if (r+c)%2 == 0 {
						bitVal = !bitVal
					}
					matrix[r][c] = bitVal
				}
			}
		}
		upwards = !upwards
	}

	// 5. Draw Format Information (15 bits with BCH error correction)
	formatInfo := spec.formatBits
	for k := 0; k < 15; k++ {
		bit := (formatInfo & (1 << k)) != 0

		// Top-left copy
		if k <= 5 {
			matrix[k][8] = bit
		} else if k == 6 {
			matrix[7][8] = bit
		} else if k == 7 {
			matrix[8][8] = bit
		} else if k == 8 {
			matrix[8][7] = bit
		} else {
			matrix[8][14-k] = bit
		}

		// Split across bottom-left and top-right copy
		if k <= 6 {
			matrix[dim-1-k][8] = bit
		} else {
			matrix[8][dim-8+(k-7)] = bit
		}
	}

	// 6. Scale modules to 384-dot lines based on QRModuleSize
	modSize := reg.QRModuleSize
	if modSize < 1 {
		modSize = 3
	} else if modSize > 12 {
		modSize = 12
	}

	quietZone := 4 // Standard 4-module quiet zone
	totalDim := dim + quietZone*2
	totalDots := totalDim * modSize

	leftMargin := reg.BarcodeLeftSpace + reg.LeftMargin
	if leftMargin+totalDots > 384 {
		leftMargin = (384 - totalDots) / 2
		if leftMargin < 0 {
			leftMargin = 0
		}
	}

	renderedRows := make([][]uint8, totalDots)
	for i := range renderedRows {
		renderedRows[i] = make([]uint8, 384)
	}

	for my := 0; my < dim; my++ {
		for mx := 0; mx < dim; mx++ {
			if matrix[my][mx] {
				pixelStartY := (my + quietZone) * modSize
				pixelStartX := leftMargin + (mx+quietZone)*modSize
				for dy := 0; dy < modSize; dy++ {
					rowIdx := pixelStartY + dy
					if rowIdx < len(renderedRows) {
						for dx := 0; dx < modSize; dx++ {
							colIdx := pixelStartX + dx
							if colIdx < 384 {
								renderedRows[rowIdx][colIdx] = 1
							}
						}
					}
				}
			}
		}
	}

	return renderedRows
}

func appendBits(buf *[]bool, val uint32, count int) {
	for i := count - 1; i >= 0; i-- {
		*buf = append(*buf, (val&(1<<i)) != 0)
	}
}

func drawFinderWithSeparator(matrix, reserved [][]bool, startX, startY, dim int) {
	for y := -1; y <= 7; y++ {
		for x := -1; x <= 7; x++ {
			px := startX + x
			py := startY + y
			if px >= 0 && px < dim && py >= 0 && py < dim {
				reserved[py][px] = true
				if x >= 0 && x <= 6 && y >= 0 && y <= 6 {
					isBorder := (x == 0 || x == 6 || y == 0 || y == 6)
					isCore := (x >= 2 && x <= 4 && y >= 2 && y <= 4)
					matrix[py][px] = isBorder || isCore
				} else {
					matrix[py][px] = false // Quiet separator
				}
			}
		}
	}
}

func drawAlignmentPattern(matrix, reserved [][]bool, centerX, centerY int) {
	for y := -2; y <= 2; y++ {
		for x := -2; x <= 2; x++ {
			px := centerX + x
			py := centerY + y
			reserved[py][px] = true
			isBorder := (x == -2 || x == 2 || y == -2 || y == 2)
			isCenter := (x == 0 && y == 0)
			matrix[py][px] = isBorder || isCenter
		}
	}
}
