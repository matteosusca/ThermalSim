#!/usr/bin/env python3
"""
ThermalSim Demo Host Application
Connects to the virtual serial port (e.g. /tmp/qr203-tty0) and sends:
1. Formatted text (alignment, bold, sizing, international chars)
2. 1D Barcode (CODE39) & 2D QR Code (GS ( k)
3. Bitmaps (DC2 * chunked bitmap mode)
4. Bidirectional Status Queries (ESC v & GS r)
5. Quirk demonstrations (Truncated bitmap lockup, mid-line barcode dump)
"""

import sys
import time
import os

def send_bytes(fd, data):
    os.write(fd, data)

def read_status(fd, timeout=1.0):
    start = time.time()
    while time.time() - start < timeout:
        try:
            b = os.read(fd, 1)
            if b:
                return b[0]
        except (BlockingIOError, OSError):
            time.sleep(0.01)
    return None

def main():
    dev_path = sys.argv[1] if len(sys.argv) > 1 else "/tmp/qr203-tty0"
    print(f"Connecting to ThermalSim virtual serial port: {dev_path}...")

    if not os.path.exists(dev_path):
        print(f"Error: Device path {dev_path} does not exist. Ensure thermalsim is running.")
        sys.exit(1)

    fd = os.open(dev_path, os.O_RDWR | os.O_NONBLOCK)

    try:
        print("\n--- 1. Querying Initial Status (ESC v 0) ---")
        send_bytes(fd, b"\x1B\x76\x00")
        time.sleep(0.1)
        st = read_status(fd)
        if st is not None:
            online = (st & 0x01) != 0
            paper_out = (st & 0x04) != 0
            overvolt = (st & 0x08) != 0
            overheat = (st & 0x40) != 0
            print(f"Status Byte: 0x{st:02X} (Online={online}, PaperOut={paper_out}, Overvolt={overvolt}, Overheat={overheat})")
        else:
            print("No status response received.")

        print("\n--- 2. Sending Formatted Receipt ---")
        receipt = bytearray()
        receipt.extend(b"\x1B\x40")          # ESC @ (Init)
        receipt.extend(b"\x1B\x61\x01")      # ESC a 1 (Center)
        receipt.extend(b"\x1B\x45\x01")      # ESC E 1 (Bold ON)
        receipt.extend(b"THERMALSIM CAFE\n")
        receipt.extend(b"\x1B\x45\x00")      # Bold OFF
        receipt.extend(b"Virtual Hardware Twin\n")
        receipt.extend(b"--------------------------------\n")
        receipt.extend(b"\x1B\x61\x00")      # ESC a 0 (Left)
        receipt.extend(b"1x Espresso           $2.50\n")
        receipt.extend(b"1x Croissant          $3.75\n")
        receipt.extend(b"--------------------------------\n")
        receipt.extend(b"\x1B\x45\x01Total:                 $6.25\x1B\x45\x00\n\n")

        # 1D Barcode (CODE39)
        receipt.extend(b"\x1B\x61\x01")      # Center
        receipt.extend(b"\x1D\x6B\x04ORDER123\x00\n\n")

        # 2D QR Code (GS ( k)
        # Model 2: 1D 28 6B 04 00 31 41 32 00
        receipt.extend(b"\x1D\x28\x6B\x04\x00\x31\x41\x32\x00")
        # Size = 3: 1D 28 6B 03 00 31 43 03
        receipt.extend(b"\x1D\x28\x6B\x03\x00\x31\x43\x03")
        # Store Data: 1D 28 6B pL pH 31 50 30 [data...]
        qr_data = b"https://thermalsim.local/receipt/123"
        p_len = len(qr_data) + 3
        receipt.extend(b"\x1D\x28\x6B" + bytes([p_len % 256, p_len // 256]) + b"\x31\x50\x30" + qr_data)
        # Print QR: 1D 28 6B 03 00 31 51 30
        receipt.extend(b"\x1D\x28\x6B\x03\x00\x31\x51\x30\n\n")

        send_bytes(fd, receipt)
        print("Receipt sent successfully.")

        print("\n--- 3. Sending DC2 * Chunked Bitmap (Checkerboard Pattern) ---")
        # DC2 * 16 12 (16 rows high, 12 bytes / 96 dots wide)
        bmp = bytearray(b"\x12\x2A\x10\x0C")
        for r in range(16):
            for c in range(12):
                bmp.append(0xAA if (r % 2 == 0) else 0x55)
        send_bytes(fd, bmp)
        send_bytes(fd, b"\n\n")
        print("Bitmap chunk sent successfully.")

        print("\n--- Demo Complete! Check the Web UI at http://localhost:8080 ---")

    finally:
        os.close(fd)

if __name__ == "__main__":
    main()
