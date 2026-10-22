# QR203 / CSN-A2 Hardware Quirks & Failure Modes Reference

This document explains the real-world firmware quirks, hardware limitations, and failure modes implemented in `ThermalSim`.

---

## 1. Blind Byte Counter Lockup (Graphics Modes)
- **Commands Affected:** `DC2 * r n`, `DC2 V nL nH`, `DC2 v nL nH`, `GS v 0 m xL xH yL yH`, `ESC * m nL nH`.
- **Hardware Behavior:** When a binary bit-image command header is received, the printer sets an internal byte counter:
  - `DC2 *`: $N = r \times n$ bytes.
  - `DC2 V/v`: $N = 48 \times (n_L + 256 n_H)$ bytes.
  - `GS v 0`: $N = (x_L + 256 x_H) \times (y_L + 256 y_H)$ bytes.
- **Quirk Reproduction:** The MCU enters `StateRawBitmap` and consumes exactly $N$ bytes. **No escape sequences (including `ESC @`, `LF`, or status commands) are parsed.**
  - If the host sends fewer bytes than declared, subsequent commands/text are swallowed as pixel bits.
  - If the host sends more bytes (e.g., trying to print 400 dots instead of 384), the extra bytes overflow into `StateNormal` and print as random CP437/GBK text and arbitrary formatting toggles.

---

## 2. SRAM Buffer Overflow & Desynchronization
- **Hardware Behavior:** The printer's micro-controller has a bounded receive ring buffer (4096 bytes on v2.69; 256 bytes on legacy clones). If the host sends unthrottled data faster than the physical thermal head can burn rows ($\sim 2.1\text{ ms}$ motor step + strobe duration) and DTR/RTS handshaking is ignored, the buffer overflows.
- **Quirk Reproduction:** Incoming UART bytes while `RingBuffer.Full()` are silently dropped. Dropping bytes mid-command desynchronizes the byte stream, turning remaining bitmap chunks or text into garbage.

---

## 3. Mid-Line Command Rejection
- **`ESC a` (Justification) & `GS L` (Left Margin):** Ignored unless `cursorCol == 0` (beginning of the line).
- **`ESC {` (Upside-down):** Only changes orientation when processed at the beginning of a line.
- **`GS k` (Barcode):** If the line buffer already contains printable characters, the command aborts and **the barcode payload bytes are dumped as normal text** on the current line.

---

## 4. Single-Line Double Width Auto-Cancel (`ESC SO`)
- Setting `ESC SO` (`0x1B 0x0E`) enables double-width printing for the current line only.
- When `LF` (`0x0A`) or `ESC DC4` is encountered, double-width is automatically disabled.

---

## 5. Bidirectional Status Query Quirks
- **`ESC v n`:**
  - Firmware v2.69 returns a single binary status byte (`Bit 0`: Online `0x01`, `Bit 2`: Paper Out `0x04`, `Bit 3`: Voltage $>9.5\text{V}$ `0x08`, `Bit 6`: Temp $>60^\circ\text{C}$ `0x40`).
  - Firmware v2.16 returns an ASCII string `P<Paper>V<Voltage>T<Degree>`.
- **`GS r n`:**
  - When the paper-end sensor detects paper out, the printer goes offline and **does not execute or reply to `GS r`**.

---

## 6. Power Supply Voltage Sag & Brownout Reset
- When high heating time (`ESC 7`) and maximum simultaneous heating dots fire on dark rows, simulated peak current causes voltage sag.
- If effective voltage drops below $4.5\text{V}$, a simulated CPU brownout reset occurs: the current line buffer and volatile RAM are cleared.
