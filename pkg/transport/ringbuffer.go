package transport

import (
	"sync"
)

// RingBuffer models the physical micro-controller SRAM receive buffer.
type RingBuffer struct {
	mu sync.Mutex

	buf      []byte
	capacity int
	head     int // Next write position
	tail     int // Next read position
	count    int

	// Overrun tracking
	droppedBytes int64

	// Hardware flow control status
	rtsBusy bool
}

// NewRingBuffer allocates a ring buffer with the specified byte capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 4096
	}
	return &RingBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

// Push adds a byte to the receive buffer.
// If the buffer is full, the byte is DROPPED and counted as an overrun error,
// matching the physical hardware UART FIFO overrun behavior.
func (rb *RingBuffer) Push(b byte) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == rb.capacity {
		rb.droppedBytes++
		return false // Dropped!
	}

	rb.buf[rb.head] = b
	rb.head = (rb.head + 1) % rb.capacity
	rb.count++

	// High watermark (75% full) asserts RTS/DTR busy
	if rb.count >= (rb.capacity*3)/4 {
		rb.rtsBusy = true
	}

	return true
}

// Pop retrieves the next byte from the receive buffer.
func (rb *RingBuffer) Pop() (byte, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return 0, false
	}

	b := rb.buf[rb.tail]
	rb.tail = (rb.tail + 1) % rb.capacity
	rb.count--

	// Low watermark (25% full) clears RTS/DTR busy
	if rb.count <= rb.capacity/4 {
		rb.rtsBusy = false
	}

	return b, true
}

// Len returns current number of bytes in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Cap returns the total capacity of the buffer.
func (rb *RingBuffer) Cap() int {
	return rb.capacity
}

// DroppedBytes returns the total count of dropped bytes due to buffer overflow.
func (rb *RingBuffer) DroppedBytes() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.droppedBytes
}

// ResetDrops resets the dropped byte counter.
func (rb *RingBuffer) ResetDrops() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.droppedBytes = 0
}

// IsRTSBusy returns whether the printer is asserting hardware flow control busy.
func (rb *RingBuffer) IsRTSBusy() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.rtsBusy
}

// Reset clears the buffer contents and resets pointers.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.count = 0
	rb.rtsBusy = false
}
