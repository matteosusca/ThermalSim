# QR203 / CSN-A2 Hardware Specification Reference

## 1. Overview & General Specifications
The **QR203** (manufactured under various OEM brandings including Cashino CSN-A2, Goojprt QR203, and EM205) is a miniature embedded 58mm direct thermal panel printer utilizing a Fujitsu FTP-628MCL101/103 class printing mechanism and a dedicated ESC/POS microcontroller board.

- **Printing Method:** Direct thermal line printing
- **Resolution:** 203.2 DPI ($8\text{ dots/mm}$)
- **Total Print Width:** $48\text{ mm} = 384\text{ dots} = 48\text{ bytes per line}$
- **Paper Roll Width:** $57.5 \pm 0.5\text{ mm}$, maximum roll diameter $39\text{–}40\text{ mm}$
- **Printing Speed:** $60\text{ mm/s}$ nominal (up to $80\text{–}90\text{ mm/s}$ at $8.5\text{V}$)
- **Head Lifetime:** $\approx 50\text{ km}$ thermal element travel
- **Input Supply Voltage:** $5\text{V to }9\text{V}$ (or $12\text{V}$ on specific variant boards)
- **Serial Interface:** RS-232C or TTL level UART (`8N1`, 8 data bits, no parity, 1 stop bit)
- **Factory Default Baud Rate:** $19200\text{ bps}$ (or $9600\text{ bps}$ on some factory runs)
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
Standard 6-pin serial connector:
1. `GND`: Ground
2. `NC`: No Connection
3. `VH`: Power Supply ($5\text{V–}9\text{V}$)
4. `GND`: Ground
5. `RXD`: Serial Receive Data (Input from host MCU/PC)
6. `TXD`: Serial Transmit Data (Output to host MCU/PC for status responses)

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
