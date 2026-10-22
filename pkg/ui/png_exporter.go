package ui

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
)

// ExportPNG writes the printed paper strip to an io.Writer as a PNG image.
func ExportPNG(w io.Writer, strip [][]uint8, includeMargins bool) error {
	dotsWidth := 384
	if len(strip) == 0 {
		// Empty paper: create a small blank strip
		strip = make([][]uint8, 30)
		for i := range strip {
			strip[i] = make([]uint8, dotsWidth)
		}
	}

	height := len(strip)
	leftRightMargin := 0
	topBottomMargin := 0
	if includeMargins {
		leftRightMargin = 24 // 3mm physical unprinted paper margin on left and right (57.5mm total width)
		topBottomMargin = 16 // Physical top/bottom receipt paper margin
	}
	totalWidth := dotsWidth + leftRightMargin*2
	totalHeight := height + topBottomMargin*2

	img := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))

	// Paper background: slight off-white thermal paper tint (0xFA, 0xFA, 0xF9)
	paperWhite := color.RGBA{R: 250, G: 250, B: 249, A: 255}
	for y := 0; y < totalHeight; y++ {
		for x := 0; x < totalWidth; x++ {
			img.Set(x, y, paperWhite)
		}
	}

	// Render printed dots
	for y, row := range strip {
		for x, darkness := range row {
			if darkness > 0 && x < dotsWidth {
				// Darkness: 255 = black, lower values = lighter thermal gray
				shade := uint8(255 - darkness)
				dotColor := color.RGBA{R: shade, G: shade, B: shade, A: 255}
				img.Set(leftRightMargin+x, topBottomMargin+y, dotColor)
			}
		}
	}

	// Draw realistic subtle paper borders and decorative tear edges if margins are included
	if includeMargins {
		borderColor := color.RGBA{R: 209, G: 213, B: 219, A: 255} // #d1d5db subtle border

		// Left and right paper outer edges
		for y := 0; y < totalHeight; y++ {
			img.Set(0, y, borderColor)
			img.Set(totalWidth-1, y, borderColor)
		}

		// Top and bottom serrated tear edges (zigzag teeth)
		for x := 0; x < totalWidth; x++ {
			// 4-pixel pitch zigzag: tooth = |(x % 8) - 4|
			tooth := (x % 8) - 4
			if tooth < 0 {
				tooth = -tooth
			}
			// Top tear edge
			img.Set(x, tooth, borderColor)
			// Bottom tear edge
			img.Set(x, totalHeight-1-tooth, borderColor)
		}
	}

	return png.Encode(w, img)
}

// SavePNG saves the printed paper strip directly to a PNG file.
func SavePNG(filePath string, strip [][]uint8, includeMargins bool) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return ExportPNG(f, strip, includeMargins)
}
