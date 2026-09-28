package protocol

import (
	"bytes"
	"testing"
	"time"
)

func createDummyIPv4Packet(id byte) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45 // IPv4, IHL=5 (20 bytes)
	pkt[1] = 0x00
	pkt[2] = 0x00
	pkt[3] = 40 // Total length
	pkt[9] = 6  // TCP
	// Src IP: 10.70.0.2
	pkt[12], pkt[13], pkt[14], pkt[15] = 10, 70, 0, 2
	// Dst IP: 1.1.1.1
	pkt[16], pkt[17], pkt[18], pkt[19] = 1, 1, 1, 1
	// Dummy payload
	pkt[20] = id
	return pkt
}

func TestRAFrameEncodeDecode(t *testing.T) {
	ipPkt := createDummyIPv4Packet(42)
	seq := uint32(100500)

	framed := EncodeRAFrame(seq, ipPkt)
	if !IsRAFrame(framed) {
		t.Fatalf("IsRAFrame returned false for valid framed packet")
	}

	decSeq, decIP, ok := DecodeRAFrame(framed)
	if !ok {
		t.Fatalf("DecodeRAFrame failed")
	}
	if decSeq != seq {
		t.Fatalf("Sequence mismatch: got %d, want %d", decSeq, seq)
	}
	if !bytes.Equal(decIP, ipPkt) {
		t.Fatalf("Decoded IP mismatch")
	}
}

func TestReorderBufferInOrder(t *testing.T) {
	reorder := NewReorderBuffer()

	p0 := createDummyIPv4Packet(0)
	p1 := createDummyIPv4Packet(1)
	p2 := createDummyIPv4Packet(2)

	// Пакеты приходят строго по порядку
	res0 := reorder.Push(0, p0)
	if len(res0) != 1 || !bytes.Equal(res0[0], p0) {
		t.Fatalf("Expected immediate yield of packet 0")
	}

	res1 := reorder.Push(1, p1)
	if len(res1) != 1 || !bytes.Equal(res1[0], p1) {
		t.Fatalf("Expected immediate yield of packet 1")
	}

	res2 := reorder.Push(2, p2)
	if len(res2) != 1 || !bytes.Equal(res2[0], p2) {
		t.Fatalf("Expected immediate yield of packet 2")
	}
}

func TestReorderBufferOutOfOrder(t *testing.T) {
	reorder := NewReorderBuffer()

	p0 := createDummyIPv4Packet(0)
	p1 := createDummyIPv4Packet(1)
	p2 := createDummyIPv4Packet(2)

	// Инициализируем первым пакетом seq=0
	res0 := reorder.Push(0, p0)
	if len(res0) != 1 {
		t.Fatalf("Expected packet 0")
	}

	// Приходит пакет seq=2 раньше, чем seq=1!
	res2 := reorder.Push(2, p2)
	if len(res2) != 0 {
		t.Fatalf("Packet 2 should be buffered waiting for packet 1, got %d packets", len(res2))
	}

	// Наконец приходит seq=1 -> должны вернуться и 1, и 2 по порядку!
	res1 := reorder.Push(1, p1)
	if len(res1) != 2 {
		t.Fatalf("Expected 2 packets after gap filled, got %d", len(res1))
	}
	if !bytes.Equal(res1[0], p1) || !bytes.Equal(res1[1], p2) {
		t.Fatalf("Packets order incorrect after gap fill")
	}
}

func TestReorderBufferStallTimeout(t *testing.T) {
	reorder := NewReorderBuffer()

	p0 := createDummyIPv4Packet(0)
	p2 := createDummyIPv4Packet(2)

	// seq=0
	_ = reorder.Push(0, p0)

	// seq=2 (seq=1 потерян навсегда)
	res := reorder.Push(2, p2)
	if len(res) != 0 {
		t.Fatalf("Packet 2 should be initially buffered")
	}

	// Ждем дольше ReorderStallTTL (40ms)
	time.Sleep(50 * time.Millisecond)

	// Пушим следующий пакет или дергаем буфер (в реальности очередной пакет триггерит stall-check)
	p3 := createDummyIPv4Packet(3)
	resStall := reorder.Push(3, p3)

	// Таймаут истек -> пакет 2 должен быть сброшен в выдачу
	found2 := false
	for _, p := range resStall {
		if bytes.Equal(p, p2) {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Fatalf("Packet 2 was not yielded after stall timeout")
	}
}
