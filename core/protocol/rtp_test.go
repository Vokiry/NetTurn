package protocol

import (
	"bytes"
	"testing"

	"github.com/Vokiry/NetTurn/core"
	"github.com/Vokiry/NetTurn/core/crypto"
)

func TestWrapUnwrapRTP(t *testing.T) {
	key, err := crypto.DeriveKey("testSecretPassword")
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	cfgAudio := NewRTPConfig(core.ObfsAudio)
	stateAudio := NewRTPState()

	originalPayload := []byte("Hello, this is an IP packet test payload!")

	// 1. Упаковка в RTP
	wire, err := WrapPacket(key, originalPayload, cfgAudio, stateAudio)
	if err != nil {
		t.Fatalf("WrapPacket failed: %v", err)
	}

	if !IsRTPPacket(wire) {
		t.Fatalf("IsRTPPacket returned false for valid wire packet")
	}

	// 2. Распаковка из RTP
	dst := make([]byte, 1500)
	n, err := UnwrapPacket(key, wire, dst)
	if err != nil {
		t.Fatalf("UnwrapPacket failed: %v", err)
	}

	if !bytes.Equal(dst[:n], originalPayload) {
		t.Fatalf("Payload mismatch: got %q, want %q", string(dst[:n]), string(originalPayload))
	}

	// 3. Проверка защиты от неверного ключа
	wrongKey, _ := crypto.DeriveKey("wrongSecretPassword")
	_, err = UnwrapPacket(wrongKey, wire, dst)
	if err == nil {
		t.Fatalf("Unwrap with wrong key should fail")
	}

	// 4. Проверка повреждения пакета
	corrupted := append([]byte(nil), wire...)
	corrupted[RTPHeaderLen+2] ^= 0xFF // Инвертируем байт шифротекста
	_, err = UnwrapPacket(key, corrupted, dst)
	if err == nil {
		t.Fatalf("Unwrap with corrupted ciphertext should fail Poly1305 authentication")
	}
}

func TestRTPVideoMode(t *testing.T) {
	key, _ := crypto.DeriveKey("videoTestPass")
	cfgVideo := NewRTPConfig(core.ObfsVideo)
	stateVideo := NewRTPState()

	if cfgVideo.PayloadType != PayloadTypeVideo {
		t.Errorf("Expected PayloadType %d, got %d", PayloadTypeVideo, cfgVideo.PayloadType)
	}

	payload := []byte("VP8 video frame encapsulated dummy IP packet")
	wire, err := WrapPacket(key, payload, cfgVideo, stateVideo)
	if err != nil {
		t.Fatalf("WrapPacket video failed: %v", err)
	}

	if !IsRTPPacket(wire) {
		t.Fatalf("IsRTPPacket video returned false")
	}

	dst := make([]byte, 1500)
	n, err := UnwrapPacket(key, wire, dst)
	if err != nil {
		t.Fatalf("UnwrapPacket video failed: %v", err)
	}
	if !bytes.Equal(dst[:n], payload) {
		t.Fatalf("Payload mismatch in video mode")
	}
}
