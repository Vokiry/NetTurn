package vk

import (
	"testing"
	"time"
)

func TestCleanCallHash(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://vk.com/call/join/vPT-ovf0Q_lkaKXi2vaRJK1JJgaOwBYjelhuAMQll1s",
			expected: "vPT-ovf0Q_lkaKXi2vaRJK1JJgaOwBYjelhuAMQll1s",
		},
		{
			input:    "https://vk.ru/call/join/abc12345?extra=param#fragment",
			expected: "abc12345",
		},
		{
			input:    "vPT-ovf0Q_lkaKXi2vaRJK1JJgaOwBYjelhuAMQll1s",
			expected: "vPT-ovf0Q_lkaKXi2vaRJK1JJgaOwBYjelhuAMQll1s",
		},
		{
			input:    "  https://vk.com/call/join/xyz987/  ",
			expected: "xyz987",
		},
	}

	for _, tc := range cases {
		got := CleanCallHash(tc.input)
		if got != tc.expected {
			t.Errorf("CleanCallHash(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestGenerateRandomName(t *testing.T) {
	name := GenerateRandomName()
	if len(name) == 0 {
		t.Fatalf("GenerateRandomName returned empty string")
	}
}

func TestTurnCredentialsExpiry(t *testing.T) {
	credsFresh := &TurnCredentials{
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if credsFresh.IsExpired() {
		t.Errorf("Fresh credentials should not be expired")
	}

	// Истекающие через 30 секунд креденшелы должны считаться истекшими
	// из-за защитного запаса в 60 секунд
	credsAlmostExpired := &TurnCredentials{
		ExpiresAt: time.Now().Add(30 * time.Second),
	}
	if !credsAlmostExpired.IsExpired() {
		t.Errorf("Credentials expiring in 30s should be considered expired (safety margin is 60s)")
	}
}

func TestCallPool(t *testing.T) {
	links := []string{
		"https://vk.com/call/join/hash1",
		"https://vk.com/call/join/hash2",
	}

	pool := NewCallPool(nil, links)
	if len(pool.calls) != 2 {
		t.Fatalf("Expected 2 calls in pool, got %d", len(pool.calls))
	}

	// Проверяем pickNextHealthy
	e1, err := pool.pickNextHealthy()
	if err != nil {
		t.Fatalf("pickNextHealthy error: %v", err)
	}
	if e1.hash != "hash1" {
		t.Errorf("Expected hash1, got %s", e1.hash)
	}

	e2, err := pool.pickNextHealthy()
	if err != nil {
		t.Fatalf("pickNextHealthy error: %v", err)
	}
	if e2.hash != "hash2" {
		t.Errorf("Expected hash2, got %s", e2.hash)
	}

	// Проверяем ротацию и блэклист
	pool.recordFailure("hash1")
	pool.recordFailure("hash1")
	pool.recordFailure("hash1") // 3 ошибки -> blacklist

	e3, err := pool.pickNextHealthy()
	if err != nil {
		t.Fatalf("pickNextHealthy error: %v", err)
	}
	if e3.hash != "hash2" {
		t.Errorf("Expected hash2 because hash1 is blacklisted, got %s", e3.hash)
	}
}
