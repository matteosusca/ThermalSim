package ui

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"
	"thermalsim/pkg/printer"
	"thermalsim/pkg/transport"
)

//go:embed static/*
var staticFS embed.FS

// WebServer provides the real-time HTTP visualization and virtual control interface.
type WebServer struct {
	addr      string
	prof      printer.Profile
	head      *printer.ThermalHead
	reg       *printer.Registers
	fsm       *printer.FSM
	ringBuf   *transport.RingBuffer

	mu        sync.Mutex
	sseClients map[chan string]struct{}
}

// NewWebServer initializes the Web UI server.
func NewWebServer(addr string, prof printer.Profile, head *printer.ThermalHead, reg *printer.Registers, fsm *printer.FSM, ringBuf *transport.RingBuffer) *WebServer {
	ws := &WebServer{
		addr:       addr,
		prof:       prof,
		head:       head,
		reg:        reg,
		fsm:        fsm,
		ringBuf:    ringBuf,
		sseClients: make(map[chan string]struct{}),
	}
	head.AddListener(ws)
	fsm.SetEventCallback(ws.onFSMEvent)
	return ws
}

// Start launches the HTTP server in a background goroutine.
func (ws *WebServer) Start() error {
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("failed to load embedded static filesystem: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))
	mux.HandleFunc("/api/stream", ws.handleSSEStream)
	mux.HandleFunc("/api/receipt.png", ws.handleDownloadPNG)
	mux.HandleFunc("/api/paper", ws.handlePaperData)
	mux.HandleFunc("/api/clear", ws.handleClear)
	mux.HandleFunc("/api/feed", ws.handleFeed)
	mux.HandleFunc("/api/faults", ws.handleFaults)
	mux.HandleFunc("/api/power", ws.handlePower)
	mux.HandleFunc("/api/inject", ws.handleInject)

	// Background ticker for live hardware status broadcast (every 250ms)
	go ws.statusBroadcastLoop()

	server := &http.Server{
		Addr:    ws.addr,
		Handler: mux,
	}

	go func() {
		_ = server.ListenAndServe()
	}()

	return nil
}

// OnRowBurned implements printer.ThermalHeadListener.
func (ws *WebServer) OnRowBurned(rowIndex int, row []uint8) {
	dotsHex := hex.EncodeToString(row)
	payload := fmt.Sprintf(`{"index":%d,"dots":"%s"}`, rowIndex, dotsHex)
	ws.broadcastSSE("row", payload)
}

// OnPaperCleared implements printer.ThermalHeadListener.
func (ws *WebServer) OnPaperCleared() {
	ws.broadcastSSE("clear", "{}")
}

// OnStatusChanged implements printer.ThermalHeadListener.
func (ws *WebServer) OnStatusChanged() {}

func (ws *WebServer) onFSMEvent(e printer.FSMEvent) {
	stateName := "STATE_NORMAL"
	switch e.State {
	case printer.StateESC:
		stateName = "STATE_ESC"
	case printer.StateGS:
		stateName = "STATE_GS"
	case printer.StateDC2:
		stateName = "STATE_DC2"
	case printer.StateFS:
		stateName = "STATE_FS"
	case printer.StateParam:
		stateName = "STATE_PARAM"
	case printer.StateRawBitmap:
		stateName = "STATE_RAW_BITMAP"
	case printer.StateTabStops:
		stateName = "STATE_TAB_STOPS"
	case printer.StateBarcodeFormat1, printer.StateBarcodeFormat2Len, printer.StateBarcodeFormat2Data:
		stateName = "STATE_BARCODE"
	case printer.StateQRPrefix, printer.StateQRPayload:
		stateName = "STATE_QR_CODE"
	}

	payload := fmt.Sprintf(`{"byte":%d,"state":"%s","desc":"%s","hasData":%t,"cursorX":%d}`,
		e.Byte, stateName, e.Desc, e.LineHasData, e.CursorX)
	ws.broadcastSSE("fsm", payload)
}

func (ws *WebServer) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	clientChan := make(chan string, 8192)
	ws.mu.Lock()
	ws.sseClients[clientChan] = struct{}{}
	ws.mu.Unlock()

	defer func() {
		ws.mu.Lock()
		delete(ws.sseClients, clientChan)
		ws.mu.Unlock()
	}()

	// Send current existing paper rows to client on initial connection
	strip := ws.head.GetPaperStrip()
	for idx, row := range strip {
		dotsHex := hex.EncodeToString(row)
		fmt.Fprintf(w, "event: row\ndata: {\"index\":%d,\"dots\":\"%s\"}\n\n", idx, dotsHex)
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-clientChan:
			fmt.Fprint(w, msg)
			// Drain any immediately pending messages before flushing to prevent syscall lag
		drainLoop:
			for {
				select {
				case next := <-clientChan:
					fmt.Fprint(w, next)
				default:
					break drainLoop
				}
			}
			flusher.Flush()
		}
	}
}

func (ws *WebServer) handlePaperData(w http.ResponseWriter, r *http.Request) {
	strip := ws.head.GetPaperStrip()
	w.Header().Set("Content-Type", "application/json")
	rows := make([]string, len(strip))
	for i, r := range strip {
		rows[i] = hex.EncodeToString(r)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"totalRows": len(strip),
		"rows":      rows,
	})
}

func (ws *WebServer) broadcastSSE(event, data string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	for ch := range ws.sseClients {
		select {
		case ch <- msg:
		default:
			// Drop if client buffer is backed up
		}
	}
}

func (ws *WebServer) statusBroadcastLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		var led int
		var temp, volt float64
		ws.fsm.WithRegisters(func(reg *printer.Registers, head *printer.ThermalHead) {
			led = head.CurrentLEDState(reg)
			temp = reg.HeadTemperature
			volt = reg.SupplyVoltage
		})
		data := fmt.Sprintf(`{"led":%d,"temperature":%.1f,"voltage":%.1f,"bufferLen":%d,"bufferCap":%d,"droppedBytes":%d}`,
			led, temp, volt, ws.ringBuf.Len(), ws.ringBuf.Cap(), ws.ringBuf.DroppedBytes())
		ws.broadcastSSE("status", data)
	}
}

func (ws *WebServer) handleDownloadPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	strip := ws.head.GetPaperStrip()
	_ = ExportPNG(w, strip, true)
}

func (ws *WebServer) handleClear(w http.ResponseWriter, r *http.Request) {
	ws.head.ClearPaper()
	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handleFeed(w http.ResponseWriter, r *http.Request) {
	ws.fsm.WithRegisters(func(reg *printer.Registers, head *printer.ThermalHead) {
		if reg.PanelKeyEnabled {
			head.FeedRows(30, reg)
		}
	})
	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handleFaults(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PaperState  *int     `json:"paperState"`
		PlatenOpen  *bool    `json:"platenOpen"`
		Temperature *float64 `json:"temperature"`
		Voltage     *float64 `json:"voltage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ws.fsm.WithRegisters(func(reg *printer.Registers, _ *printer.ThermalHead) {
		if body.PaperState != nil {
			reg.PaperState = printer.PaperStatus(*body.PaperState)
		}
		if body.PlatenOpen != nil {
			reg.PlatenOpen = *body.PlatenOpen
		}
		if body.Temperature != nil {
			reg.HeadTemperature = *body.Temperature
		}
		if body.Voltage != nil {
			reg.SupplyVoltage = *body.Voltage
		}
	})

	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handlePower(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Power bool `json:"power"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ws.fsm.WithRegisters(func(reg *printer.Registers, _ *printer.ThermalHead) {
		if !body.Power {
			// Powered off
			reg.Online = false
		} else {
			// Powered on / reboot
			reg.Reset(ws.prof)
			ws.ringBuf.Reset()
		}
	})
	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handleInject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Test string `json:"test"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var payload []byte
	switch body.Test {
	case "sample":
		payload = append(payload, []byte("\x1B\x40")...) // ESC @ Init
		payload = append(payload, []byte("\x1B\x61\x01")...) // Center
		payload = append(payload, []byte("\x1B\x45\x01ThermalSim QR203\x1B\x45\x00\n")...)
		payload = append(payload, []byte("Micro-Panel Twin\n")...)
		payload = append(payload, []byte("--------------------------------\n")...)
		payload = append(payload, []byte("\x1B\x61\x00")...) // Left
		payload = append(payload, []byte("Item 1: Thermal Paper   $2.50\n")...)
		payload = append(payload, []byte("Item 2: Serial Cable    $8.00\n")...)
		payload = append(payload, []byte("\x1B\x45\x01Total:                 $10.50\x1B\x45\x00\n\n")...)
		// Barcode CODE39: GS k 4 1 2 3 4 NUL
		payload = append(payload, []byte("\x1D\x6B\x041234\x00\n\n")...)

	case "selftest":
		payload = []byte("\x12\x54") // DC2 T

	case "quirk_lockup":
		// DC2 * 10 48 expects 480 bytes, but we only send 100 bytes followed by text
		payload = append(payload, []byte("\x12\x2A\x0A\x30")...) // DC2 * 10 48
		dummyBytes := make([]byte, 100)
		for i := range dummyBytes {
			dummyBytes[i] = 0xAA
		}
		payload = append(payload, dummyBytes...)
		payload = append(payload, []byte("THIS TEXT IS SWALLOWED AS BITMAP!\n")...)

	case "quirk_barcode":
		// Non-empty line followed by GS k -> dumps barcode payload as normal text!
		payload = append(payload, []byte("MidLineText:\x1D\x6B\x04BARCODE-DUMP\x00\n")...)

	case "quirk_overflow":
		// Flood buffer with unthrottled bitmap rows
		for i := 0; i < 30; i++ {
			payload = append(payload, []byte("\x12\x2A\x04\x30")...)
			for j := 0; j < 4*48; j++ {
				payload = append(payload, 0xFF)
			}
		}
	}

	for _, b := range payload {
		ws.ringBuf.Push(b)
	}

	w.WriteHeader(http.StatusOK)
}
