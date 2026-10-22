package transport

import (
	"io"
	"sync"
	"time"
	"thermalsim/pkg/printer"
)

// Bridge coordinates data flow between the transport I/O, the SRAM ring buffer, and the printer FSM.
type Bridge struct {
	rw         io.ReadWriter
	ringBuf    *RingBuffer
	fsm        *printer.FSM
	txChan     <-chan byte
	stopChan   chan struct{}
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// NewBridge creates a new unified bridge.
func NewBridge(rw io.ReadWriter, ringBuf *RingBuffer, fsm *printer.FSM) *Bridge {
	return &Bridge{
		rw:       rw,
		ringBuf:  ringBuf,
		fsm:      fsm,
		txChan:   fsm.TxChannel(),
		stopChan: make(chan struct{}),
	}
}

// Start launches the concurrent I/O intake, MCU execution loop, and TX transmitter.
func (b *Bridge) Start() {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.mu.Unlock()

	b.wg.Add(3)

	// 1. Ingress Loop: Read raw bytes from serial/network into SRAM RingBuffer
	go func() {
		defer b.wg.Done()
		buf := make([]byte, 256)
		for {
			select {
			case <-b.stopChan:
				return
			default:
				n, err := b.rw.Read(buf)
				if n > 0 {
					for i := 0; i < n; i++ {
						b.ringBuf.Push(buf[i])
					}
				}
				if err != nil {
					return
				}
			}
		}
	}()

	// 2. MCU Parser Loop: Pull bytes one-by-one from RingBuffer into FSM.Step(b)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-b.stopChan:
				return
			default:
				byteVal, ok := b.ringBuf.Pop()
				if ok {
					b.fsm.Step(byteVal)
				} else {
					// Buffer empty: small sleep to prevent CPU spinning
					time.Sleep(100 * time.Microsecond)
				}
			}
		}
	}()

	// 3. Egress Loop: Forward status bytes from FSM TX back to host
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-b.stopChan:
				return
			case txByte, ok := <-b.txChan:
				if !ok {
					return
				}
				_, _ = b.rw.Write([]byte{txByte})
			}
		}
	}()
}

// Stop gracefully stops the bridge goroutines.
func (b *Bridge) Stop() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.running = false
	close(b.stopChan)
	b.mu.Unlock()

	b.wg.Wait()
}
