package qr203

import (
	"fmt"
	"thermalsim/pkg/printer"
)

// GenerateSelfTestPage builds the exact hardware diagnostic self-test ticket.
func GenerateSelfTestPage(prof printer.Profile, reg *printer.Registers) [][]uint8 {
	linesA := []string{
		"********************************",
		"      SELFTEST / PRINT TEST     ",
		"********************************",
		fmt.Sprintf("Model      : %s (CSN-A2)", prof.Name()),
		fmt.Sprintf("Version    : %s", prof.FirmwareVersion()),
		fmt.Sprintf("Interface  : RS232/TTL %dbps", prof.DefaultBaudRate()),
		fmt.Sprintf("Print Width: 48mm (384 dots)"),
		fmt.Sprintf("Font Matrix: ANK 12x24 / 9x17"),
		fmt.Sprintf("Voltage/Tmp: %.1fV / %.1fC", reg.SupplyVoltage, reg.HeadTemperature),
		"Codepage   : PC437 / GB18030",
		"--------------------------------",
		"Font A (12x24, 32 cols):",
		"!\"#$%&'()*+,-./0123456789:;<=>?",
		"@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_",
		"`abcdefghijklmnopqrstuvwxyz{|}~",
	}

	linesB := []string{
		"Font B (9x17, 42 cols):",
		"!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJ",
		"KLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrst",
		"uvwxyz{|}~ 0123456789 QR203 Native 9x17",
	}

	linesFooter := []string{
		"--------------------------------",
		"CP437 Ext: Çüéâäàç£¥€±÷°░▒▓█",
		"********************************",
		"      SELF TEST COMPLETED       ",
		"********************************",
	}

	totalRows := make([][]uint8, 0, 700)

	appendLineFontA := func(line string) {
		lineRows := make([][]uint8, 30)
		for r := range lineRows {
			lineRows[r] = make([]uint8, 384)
		}
		colIdx := 0
		for _, ch := range line {
			if colIdx >= 32 {
				break
			}
			glyph := RenderRuneFontA(ch)
			charLeft := colIdx * 12
			for gy := 0; gy < 24; gy++ {
				for gx := 0; gx < 12; gx++ {
					if glyph[gy*12+gx] > 0 {
						lineRows[gy][charLeft+gx] = 1
					}
				}
			}
			colIdx++
		}
		totalRows = append(totalRows, lineRows...)
	}

	appendLineFontB := func(line string) {
		lineRows := make([][]uint8, 22)
		for r := range lineRows {
			lineRows[r] = make([]uint8, 384)
		}
		colIdx := 0
		for _, ch := range line {
			if colIdx >= 42 {
				break
			}
			glyph := RenderRuneFontB(ch)
			charLeft := colIdx * 9
			for gy := 0; gy < 17; gy++ {
				for gx := 0; gx < 9; gx++ {
					if glyph[gy*9+gx] > 0 {
						lineRows[gy][charLeft+gx] = 1
					}
				}
			}
			colIdx++
		}
		totalRows = append(totalRows, lineRows...)
	}

	for _, line := range linesA {
		appendLineFontA(line)
	}
	for _, line := range linesB {
		appendLineFontB(line)
	}
	for _, line := range linesFooter {
		appendLineFontA(line)
	}

	return totalRows
}
