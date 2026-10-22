package transport

import (
	"testing"
)

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer(4)

	if rb.Cap() != 4 || rb.Len() != 0 {
		t.Fatalf("expected cap 4, len 0; got cap %d, len %d", rb.Cap(), rb.Len())
	}

	// Push 3 bytes
	if !rb.Push(0x10) || !rb.Push(0x20) || !rb.Push(0x30) {
		t.Fatal("expected pushes to succeed")
	}
	if rb.Len() != 3 {
		t.Fatalf("expected len 3, got %d", rb.Len())
	}

	// Read 2 bytes
	b, ok := rb.Pop()
	if !ok || b != 0x10 {
		t.Fatalf("expected 0x10, got 0x%02X", b)
	}
	b, ok = rb.Pop()
	if !ok || b != 0x20 {
		t.Fatalf("expected 0x20, got 0x%02X", b)
	}
	if rb.Len() != 1 {
		t.Fatalf("expected len 1, got %d", rb.Len())
	}
}

func TestRingBufferOverflowDrop(t *testing.T) {
	rb := NewRingBuffer(3)

	if !rb.Push('A') || !rb.Push('B') || !rb.Push('C') {
		t.Fatal("first 3 pushes must succeed")
	}

	// 4th and 5th pushes must fail and increment droppedBytes
	if rb.Push('D') {
		t.Fatal("4th push must fail when full")
	}
	if rb.Push('E') {
		t.Fatal("5th push must fail when full")
	}

	if rb.DroppedBytes() != 2 {
		t.Fatalf("expected 2 dropped bytes, got %d", rb.DroppedBytes())
	}

	// Drain buffer
	b, _ := rb.Pop()
	if b != 'A' {
		t.Fatalf("expected 'A', got '%c'", b)
	}

	// Now we can push 'F'
	if !rb.Push('F') {
		t.Fatal("push must succeed after pop")
	}
}

func TestRingBufferWatermarks(t *testing.T) {
	rb := NewRingBuffer(8)

	// High watermark: 75% of 8 = 6
	for i := 0; i < 5; i++ {
		rb.Push(byte(i))
	}
	if rb.IsRTSBusy() {
		t.Fatal("RTS should not be busy at 5/8")
	}

	rb.Push(byte(5))
	if !rb.IsRTSBusy() {
		t.Fatal("RTS should be busy at 6/8")
	}

	// Drain down to 25% (2)
	for i := 0; i < 4; i++ {
		rb.Pop()
	}
	// Len is now 2 <= 8/4 = 2 -> cleared
	if rb.IsRTSBusy() {
		t.Fatal("RTS should be cleared at 2/8")
	}
}
