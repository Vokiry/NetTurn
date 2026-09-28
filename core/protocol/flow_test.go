package protocol

import (
	"encoding/binary"
	"testing"
	"time"
)

func createTCPPacket(srcIP, dstIP [4]byte, srcPort, dstPort uint16) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45 // IPv4, IHL=5
	pkt[9] = 6    // TCP
	copy(pkt[12:16], srcIP[:])
	copy(pkt[16:20], dstIP[:])
	binary.BigEndian.PutUint16(pkt[20:22], srcPort)
	binary.BigEndian.PutUint16(pkt[22:24], dstPort)
	return pkt
}

func TestFlowKeySymmetry(t *testing.T) {
	ipClient := [4]byte{10, 70, 0, 2}
	ipServer := [4]byte{93, 184, 216, 34}
	portClient := uint16(54321)
	portServer := uint16(443)

	// Uplink пакет: Client -> Server
	uplinkPkt := createTCPPacket(ipClient, ipServer, portClient, portServer)
	uplinkHash := FlowKey(uplinkPkt)

	// Downlink пакет: Server -> Client
	downlinkPkt := createTCPPacket(ipServer, ipClient, portServer, portClient)
	downlinkHash := FlowKey(downlinkPkt)

	if uplinkHash != downlinkHash {
		t.Fatalf("FlowKey must be strictly symmetric! Uplink=%d, Downlink=%d", uplinkHash, downlinkHash)
	}
}

func TestFlowTable(t *testing.T) {
	table := NewFlowTable[string](50 * time.Millisecond)

	key := uint64(123456789)
	targetWorker := "worker-3"

	// Установка привязки
	table.Set(key, targetWorker)

	val, found := table.Get(key)
	if !found || val != targetWorker {
		t.Fatalf("Expected %s, got %s (found=%v)", targetWorker, val, found)
	}

	// Ждем истечения TTL
	time.Sleep(60 * time.Millisecond)

	_, foundAfterExp := table.Get(key)
	if foundAfterExp {
		t.Fatalf("Flow record should have expired")
	}
}

func TestIPv4SourceMatches(t *testing.T) {
	ipClient := [4]byte{10, 70, 0, 2}
	ipServer := [4]byte{1, 1, 1, 1}
	pkt := createTCPPacket(ipClient, ipServer, 1000, 80)

	if !IPv4SourceMatches(pkt, "10.70.0.2") {
		t.Errorf("Expected IPv4SourceMatches to be true for 10.70.0.2")
	}
	if IPv4SourceMatches(pkt, "10.70.0.3") {
		t.Errorf("Expected IPv4SourceMatches to be false for 10.70.0.3")
	}
}
