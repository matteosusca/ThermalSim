package transport

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
	"thermalsim/pkg/printer"
)

// ErrPTYUnsupported is returned on platforms without POSIX /dev/ptmx (e.g., Windows).
var ErrPTYUnsupported = errors.New("POSIX PTY virtual serial ports are not natively supported on this OS")

// Bridge coordinates data flow between the transport I/O, the SRAM ring buffer, and the printer FSM.
type Bridge struct {
	rw           io.ReadWriter
	ringBuf      *RingBuffer
	fsm          *printer.FSM
	txChan       <-chan byte
	stopChan     chan struct{}
	wg           sync.WaitGroup
	running      bool
	mu           sync.Mutex
	realTime     bool
	baudRate     int
	byteDuration time.Duration
	writers      map[io.Writer]struct{}
	writersMu    sync.RWMutex
}

// NewBridge creates a new unified bridge.
func NewBridge(rw io.ReadWriter, ringBuf *RingBuffer, fsm *printer.FSM) *Bridge {
	return &Bridge{
		rw:       rw,
		ringBuf:  ringBuf,
		fsm:      fsm,
		txChan:   fsm.TxChannel(),
		stopChan: make(chan struct{}),
		writers:  make(map[io.Writer]struct{}),
	}
}

// SetRealTime configures whether real-time UART baud clock pacing is enforced.
func (b *Bridge) SetRealTime(enable bool, baudRate int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.realTime = enable
	b.baudRate = baudRate
	if enable && baudRate > 0 {
		// 10 bits per character frame (1 start, 8 data, 1 stop bit)
		b.byteDuration = time.Duration(float64(time.Second) * 10.0 / float64(baudRate))
	} else {
		b.byteDuration = 0
	}
}

// AttachWriter registers an egress writer (e.g. an active TCP connection) to receive printer status responses.
func (b *Bridge) AttachWriter(w io.Writer) {
	b.writersMu.Lock()
	defer b.writersMu.Unlock()
	b.writers[w] = struct{}{}
}

// DetachWriter unregisters an egress writer when its connection terminates.
func (b *Bridge) DetachWriter(w io.Writer) {
	b.writersMu.Lock()
	defer b.writersMu.Unlock()
	delete(b.writers, w)
}

// PushBytes pushes incoming bytes into the SRAM ring buffer, applying real-time baud clock pacing if enabled.
func (b *Bridge) PushBytes(buf []byte) {
	for _, byteVal := range buf {
		if b.realTime && b.byteDuration > 0 {
			time.Sleep(b.byteDuration)
		}
		b.ringBuf.Push(byteVal)
	}
}

func (b *Bridge) broadcastTx(txByte byte) {
	b.writersMu.RLock()
	defer b.writersMu.RUnlock()

	data := []byte{txByte}
	if b.rw != nil {
		_, _ = b.rw.Write(data)
	}
	for w := range b.writers {
		if netConn, ok := w.(net.Conn); ok {
			_ = netConn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		}
		_, _ = w.Write(data)
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

	// 1. Ingress Loop: Read raw bytes from serial/network into SRAM RingBuffer
	if b.rw != nil {
		b.wg.Add(1)
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
						b.PushBytes(buf[:n])
					}
					if err != nil {
						select {
						case <-b.stopChan:
							return
						default:
							time.Sleep(10 * time.Millisecond)
						}
					}
				}
			}
		}()
	}

	// 2. MCU Parser Loop: Pull bytes one-by-one from RingBuffer into FSM.Step(b)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-b.stopChan:
				return
			default:
				b.fsm.CheckSleep(time.Now())
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

	// 3. Egress Loop: Forward status bytes from FSM TX back to host (PTY and TCP)
	b.wg.Add(1)
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
				b.broadcastTx(txByte)
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
	if closer, ok := b.rw.(io.Closer); ok {
		_ = closer.Close()
	}
	b.mu.Unlock()

	b.wg.Wait()
}

