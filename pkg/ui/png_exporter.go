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
	margin := 0
	if includeMargins {
		margin = 24 // 3mm paper margin on left and right
	}
	totalWidth := dotsWidth + margin*2

	img := image.NewRGBA(image.Rect(0, 0, totalWidth, height))

	// Paper background: slight off-white thermal paper tint (0xFA, 0xFA, 0xF9)
	paperWhite := color.RGBA{R: 250, G: 250, B: 249, A: 255}
	for y := 0; y < height; y++ {
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
				img.Set(margin+x, y, dotColor)
			}
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
