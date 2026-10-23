package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"thermalsim/pkg/models/qr203"
	"thermalsim/pkg/printer"
	"thermalsim/pkg/transport"
	"thermalsim/pkg/ui"
)

func main() {
	ptyPath := flag.String("pty", "/tmp/qr203-tty0", "Path for the virtual serial PTY symlink")
	tcpAddr := flag.String("tcp", ":9100", "Listen address for raw TCP socket bridge (e.g. :9100)")
	httpAddr := flag.String("http", ":8080", "Listen address for Web UI (e.g. :8080)")
	realTime := flag.Bool("realtime", false, "Enable real-time microsecond thermal strobe and motor physics delays")
	firmware := flag.String("firmware", "2.69", "Firmware revision (2.69 or 2.16)")
	bufferSize := flag.Int("buffer", 0, "Override receive SRAM ring buffer capacity in bytes (0 for profile default)")
	inputFile := flag.String("in", "", "Input byte stream file for headless batch execution")
	outputPNG := flag.String("out", "", "Output PNG path for headless receipt rendering")
	flag.Parse()

	// 1. Initialize Profile & Hardware Core
	var fRev printer.FirmwareRevision
	if *firmware == "2.16" {
		fRev = printer.FirmwareV216
	} else {
		fRev = printer.FirmwareV269
	}
	prof := qr203.NewQR203Profile(fRev)
	reg := printer.NewRegisters(prof)
	head := printer.NewThermalHead(prof.DotsPerLine())
	head.SetProfile(prof)
	head.SetRealTimePhysics(*realTime)

	bufCap := prof.ReceiveBufferSize()
	if *bufferSize > 0 {
		bufCap = *bufferSize
	}
	ringBuf := transport.NewRingBuffer(bufCap)
	fsm := printer.NewFSM(prof, reg, head)
	head.SetResetCallback(func() { fsm.Reset() })

	// 2. Headless Batch Mode: Process file and export PNG directly
	if *inputFile != "" {
		data, err := os.ReadFile(*inputFile)
		if err != nil {
			log.Fatalf("Failed to read input file %s: %v", *inputFile, err)
		}
		log.Printf("Executing %d bytes from %s in headless mode...", len(data), *inputFile)
		for _, b := range data {
			fsm.Step(b)
		}

		outPath := *outputPNG
		if outPath == "" {
			outPath = "receipt.png"
		}
		strip := head.GetPaperStrip()
		if err := ui.SavePNG(outPath, strip, true); err != nil {
			log.Fatalf("Failed to save output PNG to %s: %v", outPath, err)
		}
		log.Printf("Successfully rendered %d rows to %s", len(strip), outPath)
		return
	}

	// 3. Interactive Daemon Mode
	fmt.Println("================================================================")
	fmt.Printf(" ThermalSim — QR203 Thermal Printer Digital Twin (Firmware %s)\n", fRev)
	fmt.Println("================================================================")
	fmt.Printf("• Print Resolution : 203 DPI (%d dots/line across 48mm)\n", prof.DotsPerLine())
	fmt.Printf("• SRAM Buffer      : %d bytes (overrun drop tracking active)\n", bufCap)
	fmt.Printf("• Real-Time Physics: %t\n", *realTime)

	// Launch Web UI
	if *httpAddr != "" {
		webServer := ui.NewWebServer(*httpAddr, prof, head, reg, fsm, ringBuf)
		if err := webServer.Start(); err != nil {
			log.Printf("Warning: Failed to start Web UI on %s: %v", *httpAddr, err)
		} else {
			fmt.Printf("• Web Dashboard    : http://localhost%s\n", *httpAddr)
		}
	}

	// Open Virtual Serial PTY (if requested & supported)
	var pty *transport.PTYPort
	if *ptyPath != "" {
		var err error
		pty, err = transport.OpenPTY(*ptyPath)
		if err != nil {
			if errors.Is(err, transport.ErrPTYUnsupported) {
				fmt.Printf("• Virtual Serial   : Disabled (POSIX PTY not available on this OS; use Raw TCP Socket)\n")
			} else {
				log.Printf("Warning: Could not allocate PTY at %s: %v", *ptyPath, err)
			}
		} else {
			defer pty.Close()
			fmt.Printf("• Virtual Serial   : %s (Slave PTS: %s)\n", pty.DevicePath(), pty.SlavePath())
		}
	}

	// Initialize and launch unified Transport Bridge (PTY, MCU parser loop, TX routing)
	bridge := transport.NewBridge(pty, ringBuf, fsm)
	bridge.SetRealTime(*realTime, prof.DefaultBaudRate())
	bridge.Start()
	defer bridge.Stop()

	// Start Raw TCP Bridge
	if *tcpAddr != "" {
		tcpBridge, err := transport.NewTCPBridge(*tcpAddr)
		if err != nil {
			log.Printf("Warning: Failed to start TCP bridge on %s: %v", *tcpAddr, err)
		} else {
			defer tcpBridge.Close()
			fmt.Printf("• Raw TCP Socket   : %s\n", tcpBridge.Addr())
			go func() {
				for {
					conn, err := tcpBridge.AcceptConn()
					if err != nil {
						return
					}
					bridge.AttachWriter(conn)
					go func(c net.Conn) {
						defer func() {
							bridge.DetachWriter(c)
							c.Close()
						}()
						buf := make([]byte, 256)
						for {
							n, err := c.Read(buf)
							if n > 0 {
								bridge.PushBytes(buf[:n])
							}
							if err != nil {
								if err != io.EOF && !errors.Is(err, net.ErrClosed) {
									log.Printf("TCP connection error: %v", err)
								}
								return
							}
						}
					}(conn)
				}
			}()
		}
	}

	fmt.Println("\nPrinter online and ready for serial stream. Press Ctrl+C to terminate.")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down ThermalSim...")
}
