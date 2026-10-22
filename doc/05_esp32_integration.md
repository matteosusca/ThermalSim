# Integration Guide: ThermalSim & ESP32 Todo Printer

This document describes how the `esp32-todo-printer` host software connects transparently to the **ThermalSim** digital twin on PC platforms.

---

## Architecture Overview

```
                      +---------------------------------------+
                      |          Telegram Bot API             |
                      +---------------------------------------+
                                          |
                                          | (HTTPS Long Polling / aiohttp)
                                          v
                      +---------------------------------------+
                      |         esp32-todo-printer            |
                      | (TelegramListener -> Dispatcher -> Q) |
                      +---------------------------------------+
                                          |
                                          | PrintWorker (64B chunks)
                                          v
                      +---------------------------------------+
                      |       hardware.DesktopUART            |
                      +---------------------------------------+
                                    |           |
             POSIX PTY (/tmp/qr203-tty0)        | Raw TCP Socket (:9100)
                                    |           |
                                    v           v
                      +---------------------------------------+
                      |        ThermalSim Digital Twin        |
                      | - 203 DPI Head Physics                |
                      | - ESC/POS Byte FSM & CGROM            |
                      | - Live Web UI (http://localhost:8080) |
                      +---------------------------------------+
```

---

## 1. Running on Desktop (PC)

The application features a unified `main.py` entrypoint that automatically detects whether it is running under MicroPython on an ESP32 or standard Python 3 (CPython) on PC.

### Quick Start with ThermalSim

1. **Launch ThermalSim:**
   ```bash
   ./bin/thermalsim -http :8080 -pty /tmp/qr203-tty0 -tcp :9100
   ```
   Open `http://localhost:8080` in your browser to view the rolling virtual receipt.

2. **Run Todo Printer on PC:**
   ```bash
   cd esp32-todo-printer
   python3 main.py --sim
   ```
   Or connect explicitly via TCP:
   ```bash
   python3 main.py --sim --tcp 127.0.0.1:9100
   ```

3. **Run Immediate Test Print (without Telegram):**
   ```bash
   python3 test_sim_printer.py
   ```
   This immediately streams a sample `/todo` list, a formatted text note with CP437 accents, and a 384px 1-bit dithered image (rendered via Pillow).

---

## 2. Desktop Transport (`hardware/desktop_uart.py`)

The `DesktopUART` class provides a drop-in duck-typed replacement for MicroPython's `machine.UART`:

- **Method `write(data: bytes) -> int`**: Writes raw ESC/POS byte buffers to either the POSIX PTY device descriptor (`os.write`) or the TCP socket (`sock.send`), returning the exact number of bytes sent.
- **Auto-reconnection**: Automatically detects broken pipes or server restarts, attempting reconnection seamlessly.
- **Pacing**: `PrintWorker` enforces 64-byte chunks with delays, faithfully reproducing the serial flow rate of a physical micro-panel printer.

---

## 3. MicroPython Backward Compatibility

- On ESP32, `IS_MICROPYTHON` evaluates to `True`.
- The CLI parser is bypassed and `machine.UART(1, baudrate=9600, tx=18, rx=17)` and `network.WLAN` are initialized without overhead or modification.
