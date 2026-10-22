# ThermalSim — Development Roadmap & Milestones

This document outlines the phased development roadmap for the ThermalSim digital twin project.

---

## Phase 1: Core Foundation & Hardware Modeling ✅
- [x] Hardware research and datasheet acquisition (`doc/datasheets/`)
- [x] Initial documentation (`AGENTS.md`, `ROADMAP.md`, `ARCHITECTURE.md`, `doc/01_qr203_hardware_spec.md`)
- [ ] Core interfaces: `printer.Profile`, `printer.FirmwareRevision`
- [ ] Thermal printhead physics model: `printer.ThermalHead` (384-dot linear array, heating energy, cooling curve, stepper motor timing)
- [ ] Internal registers: `printer.Registers` (modes, alignment, line spacing, margins, volatile/non-volatile memory)
- [ ] Bounded UART SRAM ring buffer: `transport.RingBuffer` (drop tracking, DTR/RTS flow control)

---

## Phase 2: Font ROMs, Code Pages & Symbologies
- [ ] Font A ($12 \times 24$) and Font B ($9 \times 17$) bitmapped glyph tables
- [ ] International character sets (`ESC R 0..15`) & code pages (`ESC t 0..47` including CP437, CP850, Katakana, WPC1252)
- [ ] Multi-byte Chinese/GB18030/CJK 24x24 dot generator
- [ ] 1D Barcode symbology generator (`GS k` formats 1 & 2 for UPC, EAN, CODE39, ITF, CODABAR, CODE128)
- [ ] 2D QR Code generator (`GS ( k` Model 1/2 with module scaling & error correction)
- [ ] Self-test receipt pattern generator (`DC2 T` / FEED key on power up)

---

## Phase 3: Byte-Accurate State Machine & Quirk Engine
- [ ] Sequential FSM dispatcher: `FSM.Step(b byte)`
- [ ] Standard text formatting commands (`ESC !`, `GS !`, `ESC -`, `ESC E`, `ESC G`, `GS B`, `ESC {`, `ESC V`, `ESC SP`, `ESC SO`, `ESC DC4`)
- [ ] Paper movement & line spacing (`LF`, `CR`, `HT`, `FF`, `ESC J`, `ESC d`, `ESC 2`, `ESC 3`, `GS L`, `ESC $`, `ESC B`, `ESC D`)
- [ ] All 7 bitmap graphics modes (`ESC *`, `GS v 0`, `GS *`, `GS /`, `DC2 *`, `DC2 V`, `DC2 v`, `FS q`, `FS p`)
- [ ] Hardware quirks & failure modes:
  - Blind byte-counter lockup & command swallowing on incomplete payloads
  - Extra pixel data spillover into normal text
  - Mid-line command rejection (`ESC a`, `GS L`, `ESC {`, `GS k`)
  - Receive buffer overflow desynchronization
  - Firmware dialect differences (`v2.16` vs `v2.69`: `ESC 8` arity, status command formatting, `ESC !` bit 1)
  - Voltage sag, overheat cutoff, and sleep/wakeup delay byte drops

---

## Phase 4: Virtual Serial Transport Bridge
- [ ] Native Linux PTY allocation (`/dev/ptmx` -> `/tmp/qr203-tty0`) with raw termios
- [ ] Baud rate interrogation and framing-error / baud-mismatch simulation
- [ ] Raw TCP socket server (`:9100`) for networked testing
- [ ] Bidirectional status queries (`ESC v`, `GS r`, `GS a` ASB)

---

## Phase 5: Real-Time Web UI, Hardware Controls & Exporter
- [ ] Rolling thermal paper canvas with pixel-accurate dot shading based on thermal energy
- [ ] Interactive hardware control panel:
  - Power switch & FEED key
  - Paper status switches (Paper Present, Near-End, Paper Out)
  - Platen cover open switch
  - Supply voltage slider & brownout simulator
  - Thermal head temperature gauge & overheat trigger
  - Live RX/TX hex logic analyzer and FSM state inspector
  - Built-in test receipt & quirk injection buttons
- [ ] Headless PNG receipt exporter

---

## Phase 6: Automated Verification & Documentation
- [ ] Comprehensive unit test suite (`go test ./...`)
- [ ] Python end-to-end serial test harness (`scripts/demo_host.py`)
- [ ] Complete markdown documentation under `doc/`
- [ ] Git history review and final walkthrough
