package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"thermalsim/pkg/models/qr203"
	"thermalsim/pkg/printer"
	"thermalsim/pkg/transport"
)

func TestWebServerConcurrentOperations(t *testing.T) {
	prof := qr203.NewQR203Profile(printer.FirmwareV269)
	reg := printer.NewRegisters(prof)
	head := printer.NewThermalHead(prof.DotsPerLine())
	ringBuf := transport.NewRingBuffer(prof.ReceiveBufferSize())
	fsm := printer.NewFSM(prof, reg, head)

	ws := NewWebServer(":0", prof, head, reg, fsm, ringBuf)

	var wg sync.WaitGroup

	// 1. Concurrent FSM byte processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			for _, b := range []byte("CONCURRENT TEST LINE\n") {
				fsm.Step(b)
			}
		}
	}()

	// 2. Concurrent UI HTTP requests (Faults, Feed, Power, Paper, PNG Download, Clear)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			reqFault := httptest.NewRequest(http.MethodPost, "/api/faults", bytes.NewBufferString(`{"temperature":42.5,"voltage":7.1}`))
			recFault := httptest.NewRecorder()
			ws.handleFaults(recFault, reqFault)
			if recFault.Code != http.StatusOK {
				t.Errorf("expected 200 on handleFaults, got %d", recFault.Code)
			}

			reqFeed := httptest.NewRequest(http.MethodPost, "/api/feed", nil)
			recFeed := httptest.NewRecorder()
			ws.handleFeed(recFeed, reqFeed)

			reqPaper := httptest.NewRequest(http.MethodGet, "/api/paper", nil)
			recPaper := httptest.NewRecorder()
			ws.handlePaperData(recPaper, reqPaper)

			reqPNG := httptest.NewRequest(http.MethodGet, "/api/receipt.png", nil)
			recPNG := httptest.NewRecorder()
			ws.handleDownloadPNG(recPNG, reqPNG)

			reqPower := httptest.NewRequest(http.MethodPost, "/api/power", bytes.NewBufferString(`{"power":true}`))
			recPower := httptest.NewRecorder()
			ws.handlePower(recPower, reqPower)
		}
	}()

	wg.Wait()

	reqClear := httptest.NewRequest(http.MethodPost, "/api/clear", nil)
	recClear := httptest.NewRecorder()
	ws.handleClear(recClear, reqClear)
	if head.TotalRows() != 0 {
		t.Fatalf("expected 0 rows after clear, got %d", head.TotalRows())
	}
}
