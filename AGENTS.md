# ThermalSim — Agent Guidelines & Engineering Standards

Welcome to **ThermalSim**, the zero-knowledge, hardware-accurate digital twin project for thermal printers.
All coding agents and developers working on this codebase MUST follow these instructions.

---

## 1. Core Emulation Philosophy: "Zero-Knowledge Digital Twin"

1. **Never build a document renderer:**
   - The goal is **not** to parse ESC/POS commands into HTML/PDF or pretty-print formatted documents.
   - The engine must operate strictly byte-by-byte, replicating internal MCU registers, line buffers, dot-line strobes, and stepper motor pulses.
2. **Faithfully reproduce firmware bugs and hardware failure modes:**
   - **Never auto-recover or sanitize bad inputs:** If the host software transmits incomplete bitmap payloads, incorrect byte counts, or invalid control codes, do **not** silently recover or flush.
   - **Reproduce side-effects:** The emulator must replicate the real printer's side effects:
     - Getting stuck in raw byte consumption mode (e.g., waiting for remaining bitmap bytes and swallowing subsequent commands/text).
     - Dumping garbage CP437/GBK characters on mid-line barcode rejection or bitmap overflow.
     - Buffer overrun loss: when data arrives faster than the simulated printhead can burn and advance dots, the ring buffer drops bytes exactly like an unbuffered hardware UART ISR.
     - Thermal and power brownouts: low voltage or extreme heating parameters trigger voltage sags, head stalls, or 5-blink overheat halts.

---

## 2. Software Architecture & Design Principles

Maintain a clean, scalable architecture adhering to standard design principles:
- **Single Responsibility Principle (SRP):** Each package and struct has one reason to change.
  - `pkg/transport`: Byte transport, POSIX PTY allocation, UART timing, and SRAM ring buffer.
  - `pkg/printer`: Generic printer core (thermal head physics, line buffer, register state, byte FSM).
  - `pkg/models/<model>`: Printer-specific profiles, font ROMs, code page tables, barcode formats, and quirks.
  - `pkg/ui`: Visual presentation (rolling canvas, WebSocket streaming, virtual switchboard).
- **Separation of Concerns (SoC) & Open/Closed Principle (OCP):**
  - The transport layer knows nothing about ESC/POS.
  - Adding a new thermal printer (e.g., an 80mm Epson or Star Micronics model) must only require implementing a new `printer.Profile` and command mapping in `pkg/models/<new_model>`, with zero modifications to the transport or UI layers.
- **KISS & DRY:** Keep it simple, maintainable, and avoid unnecessary third-party dependencies. Prefer Go standard library capabilities (`net/http`, `image/png`, `embed.FS`, `syscall`).

---

## 3. Mandatory Documentation & Commit Discipline

1. **Update `doc/` after every feature:**
   - Whenever a new subsystem, command set, or quirk is added or modified, update or add the corresponding markdown documentation in `doc/`.
   - Never consider a task complete without documenting the implementation details and hardware rationale under `doc/`.
2. **Frequent, Atomic Git Commits:**
   - Commit after completing each logical component or milestone.
   - Use clear, descriptive commit messages (e.g. `feat(fsm): implement 1D barcode and QR code decoding with non-empty buffer quirk`).

---

## 4. Testing & Verification Standards

- Every state transition, command handler, and hardware quirk must have automated Go unit tests (`go test ./...`).
- Verification must include edge cases: buffer overflows, truncated graphics streams, arity mismatches across firmware revisions (`v2.16` vs `v2.69`), and physical PTY loopback communication.
