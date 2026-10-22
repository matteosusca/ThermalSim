# ThermalSim — Transport Bridge & Virtual Serial Port Guide

`ThermalSim` provides two transparent transport bridges so that existing host software (Python `pyserial`, C/C++, Node.js, Arduino test suites) can communicate with the emulator using standard APIs without modification.

---

## 1. Virtual Serial PTY Interface (`/dev/ptmx` & Symlinks)

When started with `--pty <path>` (e.g. `--pty /tmp/qr203-tty0`), `ThermalSim`:
1. Opens `/dev/ptmx` and unlocks an allocated Linux pseudo-terminal slave (`/dev/pts/N`).
2. Configures the slave terminal to raw mode (8 data bits, no parity, 1 stop bit, no echo, no CR/NL conversions).
3. Creates a symbolic link at the requested path (`/tmp/qr203-tty0`).
4. Cleans up the symlink automatically on exit.

### Connecting Host Software
```python
import serial

# Connect to ThermalSim as if it were a physical USB-to-UART cable
ser = serial.Serial('/tmp/qr203-tty0', baudrate=19200, timeout=1.0)

# Send print data
ser.write(b"Hello from Host Software!\n")

# Query status (ESC v 0)
ser.write(b"\x1B\x76\x00")
status = ser.read(1)
print(f"Printer Status: 0x{status[0]:02X}")
```

---

## 2. Raw TCP Bridge (Default Port `:9100`)

When started with `--tcp :9100`, the emulator listens for raw network print streams (the industry-standard port for raw receipt printing).
```bash
# Send raw ESC/POS byte payloads directly via netcat
nc localhost 9100 < test_receipt.bin
```

---

## 3. Microcontroller SRAM Ring Buffer & Dropped Bytes
All incoming bytes pass into a bounded SRAM ring buffer (4096 bytes on v2.69; 256 bytes on legacy clones):
- When the buffer reaches 75% capacity, RTS/DTR hardware flow control signals `BUSY`.
- If the host ignores flow control and overflows the buffer, excess bytes are dropped, exactly replicating physical UART overrun.
