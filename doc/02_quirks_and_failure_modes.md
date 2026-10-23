# QR203 / CSN-A2 Hardware Quirks & Failure Modes Reference

This document explains the real-world firmware quirks, hardware limitations, and failure modes implemented in `ThermalSim`.

---

## 1. Blind Byte Counter Lockup & Raw Stream Clamping (Graphics Modes)
- **Commands Affected:** `DC2 * r n`, `DC2 V nL nH`, `DC2 v nL nH`, `GS v 0 m xL xH yL yH`, `ESC * m nL nH`.
- **Hardware Behavior:** When a binary bit-image command header is received, the printer sets an internal byte counter:
  - `DC2 *`: $N = r \times n$ bytes.
  - `DC2 V/v`: $N = 48 \times (n_L + 256 n_H)$ bytes.
  - `GS v 0`: $N = (x_L + 256 x_H) \times (y_L + 256 y_H)$ bytes.
- **Hardware Clamping (CSN-A2 Manual p. 34):** In `GS v 0`, all $N$ declared bytes are received and consumed from the UART stream. If the host declares a width exceeding the 384-dot head ($x_L > 48$), excess dots beyond dot 384 are discarded on a dot-by-dot basis; the stream does not lose row alignment or prematurely dump remaining bytes into `StateNormal`.
- **Quirk Reproduction:** The MCU enters `StateRawBitmap` (or `StateESCStarData`) and consumes the exact declared byte count. **No escape sequences (including `ESC @`, `LF`, or status commands) are parsed.**
  - If the host sends fewer bytes than declared, subsequent commands/text are swallowed as pixel bits.
  - If the host sends incomplete streams, the printer remains locked in raw mode until the full payload arrives.
  - **`ESC *` Out-of-Range `m` Quirk (`203 User Manual` p.18):** If the mode byte $m \notin \{0, 1, 32, 33\}$ (plus $31$ on v2.16), the printer immediately aborts `ESC *` before reading `nL, nH`, processing `nL`, `nH`, and all following bytes as normal text!

---

## 2. SRAM Buffer Overflow & Real-Time Baud Pacing
- **Hardware Behavior:** The printer's micro-controller has a bounded receive ring buffer (4096 bytes on v2.69; 256 bytes on legacy clones). If the host sends unthrottled data faster than the physical thermal head can burn rows (~2.1 ms motor step + strobe duration) and DTR/RTS handshaking is ignored, the buffer overflows.
- **UART Shift Register Pacing:** In `-realtime` mode, incoming bytes are clocked at the physical baud rate (~1.04 ms/byte at 9600 bps), replicating physical UART ISR delivery and buffer watermark transitions.
- **Quirk Reproduction:** Incoming UART bytes while `RingBuffer.Full()` are silently dropped. Dropping bytes mid-command desynchronizes the byte stream, turning remaining bitmap chunks or text into garbage.

---

## 3. Mid-Line Command Rejection
- **`ESC a` (Justification) & `GS L` (Left Margin):** Ignored unless `cursorCol == 0` (beginning of the line).
- **`ESC {` (Upside-down):** Only changes orientation when processed at the beginning of a line.
- **`GS k` (Barcode):** If the line buffer already contains printable characters, the command aborts and **the barcode payload bytes are dumped as normal text** on the current line.

---

## 4. Text Formatting & Thermal Chemistry Quirks
- **Single-Line Double Width Auto-Cancel (`ESC SO`):** Setting `ESC SO` (`0x1B 0x0E`) enables double-width printing for the current line only. When `LF` (`0x0A`) or `ESC DC4` is encountered, double-width is automatically disabled.
- **Additive Dot Compositing (Thermal Chemistry):** Thermal paper darkening is a one-way chemical reaction. Using absolute horizontal movement (`ESC $`) to overprint an existing line performs a bitwise OR (`lineBuf |= glyphDot`), preserving burned black dots rather than erasing them with white background pixels.
- **Inverse Mode Character Spacing (`GS B 1` + `ESC SP n`):** When inverse (white-on-black) printing is enabled, inter-character spacing dots (`RightSideSpacing`) are inverted to solid black dots per CSN-A2 Manual p. 21.

---

## 5. Bidirectional Status Query & Sleep Mode Quirks
- **`ESC v n`:**
  - Firmware v2.69 returns a single binary status byte (`Bit 0`: Online `0x01`, `Bit 2`: Paper Out `0x04`, `Bit 3`: Voltage > 9.5V `0x08`, `Bit 6`: Temp > 60°C `0x40`).
  - Firmware v2.16 returns an ASCII string `P<Paper>V<Voltage>T<Degree>`.
  - Replies are multiplexed to both the virtual serial PTY and connected raw TCP sockets.
- **`GS r n`:**
  - When the paper-end sensor detects paper out, the printer goes offline and **does not execute or reply to `GS r`**.
- **Sleep / Standby Mode (`ESC 8 n1 n2`):**
  - When enabled, an inactivity timer tracking duration since the last byte puts the printer into low-power sleep.
  - In sleep mode, arbitrary incoming bytes are ignored until a wake byte (`0xFF`) is received.
  - Upon receiving `0xFF`, the MCU wakes but drops any bytes received within a 50 ms stabilization window.

---

## 6. Power Supply Voltage Sag & Brownout Reset
- When high heating time (`ESC 7`) and maximum simultaneous heating dots fire on dark rows, simulated peak current causes voltage sag.
- If effective voltage drops below 4.5V, a simulated CPU brownout reset occurs: the FSM state machine resets to `StateNormal`, line buffers and volatile RAM are cleared, and active profile power-on defaults are restored.
