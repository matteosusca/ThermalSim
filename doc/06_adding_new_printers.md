# Implementing Support for New Thermal Printers

This guide walks through the process of adding a new thermal printer model (such as an 80mm Epson TM-T88, Star Micronics, Rongta, or another 58mm micro-panel variant) to **ThermalSim**.

Because of ThermalSim's strict **Separation of Concerns (SoC)** and **Open/Closed Principle (OCP)** architecture, adding a new printer **never requires modifying the serial transport, Linux/macOS PTY layers, or Web UI dashboard**. You only need to define a new `printer.Profile` under `pkg/models/<model_name>`.

---

## 1. Architecture Overview

```
                        +---------------------------+
                        |  pkg/transport (PTY/TCP)  |
                        +---------------------------+
                                      |
                                      v
                        +---------------------------+
                        |    pkg/printer (Core)     |
                        |   FSM, Head, Registers    |
                        +---------------------------+
                                      |
                         implements   |  queries
                                      v
                        +---------------------------+
                        |      printer.Profile      |
                        +---------------------------+
                               /             \
                              v               v
             +-----------------------+   +-----------------------+
             |   pkg/models/qr203    |   |   pkg/models/<new>    |
             |    (Goojprt 58mm)     |   |  (e.g., Epson 80mm)   |
             +-----------------------+   +-----------------------+
```

---

## 2. Step-by-Step Implementation Guide

### Step 1: Create the Model Package

Create a new directory under `pkg/models/`:
```bash
mkdir -p pkg/models/<model_name>
```
*Example:* `pkg/models/tmt88/` or `pkg/models/pos80/`.

---

### Step 2: Implement the `printer.Profile` Interface

Create `pkg/models/<model_name>/profile.go`. Your struct must satisfy the [`printer.Profile`](../pkg/printer/profile.go) interface:

```go
package myprinter

import (
	"thermalsim/pkg/printer"
)

type MyPrinterProfile struct {
	firmware printer.FirmwareRevision
}

func NewMyPrinterProfile(fw printer.FirmwareRevision) *MyPrinterProfile {
	return &MyPrinterProfile{firmware: fw}
}

// 1. Identification & Geometry
func (p *MyPrinterProfile) Name() string {
	return "MyPrinter-80mm"
}

func (p *MyPrinterProfile) FirmwareVersion() printer.FirmwareRevision {
	return p.firmware
}

// Total horizontal printhead dots (e.g., 576 for standard 80mm, 384 for 58mm)
func (p *MyPrinterProfile) DotsPerLine() int {
	return 576
}

// Bytes per full horizontal scanline: DotsPerLine / 8 (e.g., 72 for 80mm, 48 for 58mm)
func (p *MyPrinterProfile) BytesPerLine() int {
	return 72
}

// 2. Hardware Defaults & SRAM Limits
func (p *MyPrinterProfile) DefaultBaudRate() int {
	return 19200
}

// Receive SRAM ring buffer capacity in bytes (e.g., 4096, 16384, or 65536)
func (p *MyPrinterProfile) ReceiveBufferSize() int {
	return 16384
}

// Baseline line spacing in dot rows (standard 30 dots = 3.75mm at 203 DPI)
func (p *MyPrinterProfile) DefaultLineSpacing() uint8 {
	return 30
}

// Thermal heating parameters (ESC 7 values)
func (p *MyPrinterProfile) MaxHeatingDots() uint8         { return 7 }  // 8 * (n1 + 1) dots
func (p *MyPrinterProfile) DefaultHeatingTime() uint8     { return 80 } // n2 * 10 µs
func (p *MyPrinterProfile) DefaultHeatingInterval() uint8 { return 2 }  // n3 * 10 µs

// 3. Supported Symbologies
func (p *MyPrinterProfile) SupportsQRCode() bool {
	return true
}

func (p *MyPrinterProfile) SupportsBarcode(system int) bool {
	// Return true for supported 1D systems (UPC-A, EAN13, CODE39, CODE128, etc.)
	return true
}
```

---

### Step 3: Implement CGROM Font & Code Page Rendering

Define character rendering in `pkg/models/<model_name>/fonts.go`:

```go
// RenderChar returns the 1-bit dot bitmap for an ASCII or extended character code.
// The slice length must be: fontWidth * fontHeight bytes (1 = black, 0 = white).
func (p *MyPrinterProfile) RenderChar(code byte, font printer.FontType, codePage int, intlSet int) []byte {
	switch font {
	case printer.FontA: // e.g., 12x24 dots
		return getFontA12x24(code, codePage, intlSet)
	case printer.FontB: // e.g., 9x17 dots
		return getFontB9x17(code, codePage, intlSet)
	default:
		return getFontA12x24(code, codePage, intlSet)
	}
}

// RenderKanji renders double-byte Kanji/GBK/BIG5 characters (24x24 dots).
func (p *MyPrinterProfile) RenderKanji(c1, c2 byte) []byte {
	return lookupKanji24x24(c1, c2)
}
```

> [!TIP]
> You can reuse or adapt font tables from `pkg/models/qr203/rom_fonts.go` if your printer uses standard Songti/ANK $12\times24$ or $9\times17$ dot-matrix CGROMs.

---

### Step 4: Implement Barcodes & Self-Test Generation

Implement symbology synthesis in `pkg/models/<model_name>/symbology.go`:

```go
// GenerateBarcode renders a 1D barcode into a 2D matrix of dots plus human-readable text (HRI).
func (p *MyPrinterProfile) GenerateBarcode(m int, data []byte, reg *printer.Registers) *printer.BarcodeResult {
	// Synthesize UPC, EAN, CODE39, or CODE128 pattern
	// Return &printer.BarcodeResult{Rows: scanlines, HRI: text}
}

// ProcessQRCommand handles 2D QR Code function codes (GS ( k).
func (p *MyPrinterProfile) ProcessQRCommand(fn byte, payload []byte, reg *printer.Registers) [][]uint8 {
	// Handle Fn 65 (model), Fn 67 (module size), Fn 69 (error level), Fn 80 (data), Fn 81 (print)
}

// GenerateSelfTestPage returns the full diagnostic test ticket (e.g. for DC2 T or self-test button).
func (p *MyPrinterProfile) GenerateSelfTestPage(reg *printer.Registers) [][]uint8 {
	// Return 2D bitmap lines of the hardware self-test diagnostic
}
```

---

### Step 5: Handling Model-Specific Quirks & Commands

Thermal printers often have specific proprietary dialects:
- **Paper Cutters (`GS V m` / `ESC i`):** Return visual paper cut demarcation lines.
- **Cash Drawer Kick (`ESC p m t1 t2`):** Emit an event or log the drawer strobe pulse.
- **Two-Color Printing:** Handle color selection registers (`ESC r n`).

If your printer has new ESC/POS commands not yet parsed by the generic FSM, add the parsing states in [`pkg/printer/fsm.go`](../pkg/printer/fsm.go). Because `FSM` queries the active `Profile` for capabilities, model-specific variations can be gated cleanly with interface queries.

---

### Step 6: Register the Model in CLI (`cmd/thermalsim/main.go`)

Add a `-model` CLI flag to [`cmd/thermalsim/main.go`](../cmd/thermalsim/main.go):

```go
modelFlag := flag.String("model", "qr203", "Printer model profile (qr203, pos80, tmt88)")
// ...
var prof printer.Profile
switch *modelFlag {
case "pos80":
    prof = pos80.NewPOS80Profile(fRev)
default:
    prof = qr203.NewQR203Profile(fRev)
}
```

---

### Step 7: Automated Verification

Create unit tests in `pkg/models/<model_name>/profile_test.go`:
```go
package myprinter_test

import (
	"testing"
	"thermalsim/pkg/models/myprinter"
	"thermalsim/pkg/printer"
)

func TestMyPrinterGeometry(t *testing.T) {
	p := myprinter.NewMyPrinterProfile(printer.FirmwareV269)
	if p.DotsPerLine() != 576 {
		t.Fatalf("expected 576 dots/line, got %d", p.DotsPerLine())
	}
	if p.BytesPerLine() != 72 {
		t.Fatalf("expected 72 bytes/line, got %d", p.BytesPerLine())
	}
}
```
Run tests with the race detector enabled:
```bash
go test -v -race ./pkg/models/<model_name>/...
```
