# QR203 / CSN-A2 Hardware Specification Reference

## 1. Overview & General Specifications
The **QR203** (manufactured under various OEM brandings including Cashino CSN-A2, Goojprt QR203, and EM205) is a miniature embedded 58mm direct thermal panel printer utilizing a Fujitsu FTP-628MCL101/103 class printing mechanism and a dedicated ESC/POS microcontroller board.

- **Printing Method:** Direct thermal line printing
- **Resolution:** 203.2 DPI ($8\text{ dots/mm}$)
- **Total Print Width:** $48\text{ mm} = 384\text{ dots} = 48\text{ bytes per line}$
- **Paper Roll Width:** $57.5 \pm 0.5\text{ mm}$, maximum roll diameter $39\text{–}40\text{ mm}$ ($55\text{ mm}$ on open-frame bracket)
- **Chassis Dimensions (Goojprt QR203):** $76.8 \times 77.4 \times 47.65\text{ mm}$ (panel cutout $72.8 \times 73.25\text{ mm}$, depth $34.55\text{ mm}$)
- **Printing Speed:** $50\text{–}60\text{ mm/s}$ nominal (up to $80\text{–}90\text{ mm/s}$ at $8.5\text{V–}9\text{V}$)
- **Head Lifetime:** $\approx 50\text{ km}$ thermal element travel
- **Input Supply Voltage:** $5\text{V to }9\text{V}$ ($2\text{A}$ peak) or $12\text{V}$ on industrial variants
- **Serial Interface:** RS-232C ($\pm 12\text{V}$) or TTL level UART ($0\text{–}3.3\text{V}/5\text{V}$, `8N1`) and Mini-B USB
- **Factory Default Baud Rate:** **$9600\text{ bps}$** on Goojprt QR203 (configurable via `ESC 0 n1 n2 n3 d1..dk`: `0`=9600, `1`=19200, `2`=38400, `3`=57600, `4`=115200 bps)
- **Default Heating Parameters (`ESC 7`):**
  - Goojprt 203 (v2.69): `n1=9` ($80\text{ dots}$), `n2=80` ($800\text{ }\mu\text{s}$), `n3=2` ($20\text{ }\mu\text{s}$)
  - Legacy (v2.16): `n1=7` ($64\text{ dots}$), `n2=80` ($800\text{ }\mu\text{s}$), `n3=2` ($20\text{ }\mu\text{s}$)
- **Internal SRAM Receive Buffer:**
  - Standard (Firmware v2.64–v2.69): **4096 bytes**
  - Legacy/Clone: **256 bytes** (reproduces unthrottled bitmap buffer overflow bugs)
- **Built-in Character Sets:**
  - Font A: $12 \times 24$ dots ($32$ characters per line)
  - Font B: $9 \times 17$ dots ($42$ characters per line)
  - Chinese/GB18030: $24 \times 24$ dots ($16$ characters per line)
- **Code Pages:** 48 selectable code pages via `ESC t n` (Default `0`: CP437, `1`: Katakana, `2`: CP850, `16`: WPC1252)
- **International Sets:** 16 sets via `ESC R n` (USA, France, Germany, UK, Denmark I/II, Sweden, Italy, Spain I/II, Japan, Norway, Latin America, Korea, Slovenia/Croatia, China)

---

## 2. Electrical Pinouts & Control Lines
**Goojprt QR203 (`203 User Manual` / EPM203-MRS board connectors):**
- **J3 (TTL 5-pin Molex 53047) & J4 (RS232 5-pin Molex 53047):**
  1. `GND`: Ground
  2. `TXD`: Transmit data (Printer output $\rightarrow$ Host RX)
  3. `RXD`: Receive data (Printer input $\leftarrow$ Host TX)
  4. `CTS/DSR`: Flow control input
  5. `RTS/DTR`: Flow control output
- **J5 (Power Connector):** `GND` and `VH` ($5\text{V–}9\text{V}$ DC)
- **USB Connector (Mini-B 5-pin):** `1: VBUS`, `2: D-`, `3: D+`, `4: ID`, `5: GND`

---

## 3. Front Panel Indicator & Control
- **FEED Key:** Manually advances paper when enabled via `ESC c 5 0`. When held down while powering on the unit, triggers the hardware self-test diagnostic printout.
- **Status LED Flash Sequences:**
  - 1 Flash: Normal working state
  - 2 Flashes: Mechanism disconnected or head temperature too low
  - 3 Flashes: Out of paper
  - 4 Flashes: Cutter error (for models with cutter)
  - 5 Flashes: Thermal head overheating ($>60^\circ\text{C}\text{–}65^\circ\text{C}$)
  - 6 Flashes: Platen/roller not engaged
  - 10 Flashes: Flash ROM error

---

## 4. Native CGROM Font Matrices (`pkg/models/qr203/rom_fonts.go`)
The physical QR203 / CSN-A2 stores character bitmaps inside its onboard GB18030 / ANK Character Generator ROM (CGROM):
- **Font A (`12×24` dots, `ASC24`):**
  - 12 horizontal dots $\times$ 24 vertical dots (36 bytes in column-major 3-byte format per character, stored in `fontA12x24` as 24 rows of 12-bit masks).
  - Cap top at row 5, baseline at row 20, descenders (`g, j, p, q, y, ;, ,`) extending to row 23, and diacritic space in rows `0..4` (uppercase) / `5..8` (lowercase).
  - Features native Songti/serifed proportions (`0xF0F` baseline serifs on `'A'`, vertical tick on `'2'`, raised apex on `'4'`) matching the scanned printouts in the Cashino CSN-A2 manual (Pages 27, 46–47).
- **Font B (`9×17` dots, `ASC17`):**
  - 9 horizontal dots $\times$ 17 vertical dots (`fontB9x17`, 42 characters per 384-dot line), selected via `ESC ! 1` or `ESC M 1`.
  - Cap top at row 3, baseline at row 14, descenders at rows `15..16`.
- **Extended Code Pages & International Character Sets:**
  - Native $12\times24$ and $9\times17$ glyphs for CP437, CP850, CP858 (`€`), WCP1252, and `ESC R 0..15` International substitutions, including diacritics, edge-to-edge box-drawing (`0xB0..0xDF`), block shading (`░▒▓█`), and currency/math symbols.
- **No Artificial Blur / Thermal Bleed:**
  - Dots are rendered with crisp 1:1 hardware dot accuracy (zero interpolation or artificial blur).
