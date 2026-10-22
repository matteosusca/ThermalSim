package qr203

import (
	"fmt"
	"thermalsim/pkg/printer"
)

// GenerateSelfTestPage builds the exact hardware diagnostic self-test ticket.
func GenerateSelfTestPage(prof printer.Profile, reg *printer.Registers) [][]uint8 {
	lines := []string{
		"********************************",
		"         PRINTER STATUS         ",
		"********************************",
		fmt.Sprintf("Model: %s", prof.Name()),
		fmt.Sprintf("Version: %s", prof.FirmwareVersion()),
		fmt.Sprintf("Baudrate: %d", prof.DefaultBaudRate()),
		fmt.Sprintf("Voltage: %.1fV", reg.SupplyVoltage),
		fmt.Sprintf("Head Temp: %.1fC", reg.HeadTemperature),
		"Interface: RS232 / TTL",
		"Charset: CP437 / ANK",
		"--------------------------------",
		"!\"#$%&'()*+,-./0123456789:;<=>?",
		"@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_",
		"`abcdefghijklmnopqrstuvwxyz{|}~",
		"********************************",
		"      SELF TEST COMPLETED       ",
		"********************************",
	}

	totalRows := make([][]uint8, 0, len(lines)*30)

	for _, line := range lines {
		// Render 24 dots of text + 6 dots of inter-line feed = 30 dots
		lineRows := make([][]uint8, 30)
		for r := range lineRows {
			lineRows[r] = make([]uint8, 384)
		}

		for colIdx, ch := range line {
			if colIdx >= 32 {
				break
			}
			glyph := RenderGlyphFontA(byte(ch))
			charLeft := colIdx * 12
			for gy := 0; gy < 24; gy++ {
				for gx := 0; gx < 12; gx++ {
					if glyph[gy*12+gx] > 0 {
						lineRows[gy][charLeft+gx] = 1
					}
				}
			}
		}

		totalRows = append(totalRows, lineRows...)
	}

	return totalRows
}
