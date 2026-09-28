package network

import (
	"testing"
	"time"
)

func TestParseIPv4(t *testing.T) {
	cases := []struct {
		input    string
		expected uint32
		valid    bool
	}{
		{"10.70.0.2", 0x0A460002, true},
		{"192.168.1.1", 0xC0A80101, true},
		{"invalid.ip", 0, false},
		{"", 0, false},
	}

	for _, tc := range cases {
		val, err := ParseIPv4(tc.input)
		if tc.valid && err != nil {
			t.Errorf("ParseIPv4(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ParseIPv4(%q) expected error, got nil", tc.input)
		}
		if tc.valid && val != tc.expected {
			t.Errorf("ParseIPv4(%q) = %x; want %x", tc.input, val, tc.expected)
		}
	}
}

func TestAdaptiveWatcherInit(t *testing.T) {
	watcher := NewAdaptiveWatcher(100*time.Millisecond, func(gw *GatewayInfo) {})
	if watcher.interval != 100*time.Millisecond {
		t.Errorf("Expected interval 100ms, got %v", watcher.interval)
	}
	if watcher.targetAddr != "77.88.8.8:53" {
		t.Errorf("Expected targetAddr 77.88.8.8:53, got %s", watcher.targetAddr)
	}
}

func TestFindPhysicalDefaultGateway(t *testing.T) {
	// На реальной Linux машине должен успешно обнаруживаться физический шлюз
	gw, err := FindPhysicalDefaultGateway()
	if err != nil {
		t.Logf("Physical gateway not found (may happen in container/ci): %v", err)
		return
	}

	if gw.Interface == "" || gw.GatewayIP == nil {
		t.Errorf("Invalid gateway result: %+v", gw)
	}
	t.Logf("Found default gateway: %s via %s", gw.GatewayIP.String(), gw.Interface)
}
