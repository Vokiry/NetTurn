package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	pass := "secretPassword123"
	key1, err := DeriveKey(pass)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}
	if len(key1) != KeyLen {
		t.Fatalf("Expected key len %d, got %d", KeyLen, len(key1))
	}

	// Деривация должна быть строго детерминированной
	key2, err := DeriveKey(pass)
	if err != nil {
		t.Fatalf("DeriveKey failed second time: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Fatalf("DeriveKey produced different keys for same password")
	}

	// Разные пароли должны давать разные ключи
	keyDiff, err := DeriveKey("otherPassword")
	if err != nil {
		t.Fatalf("DeriveKey failed for different password: %v", err)
	}
	if bytes.Equal(key1, keyDiff) {
		t.Fatalf("Different passwords produced identical keys")
	}

	// Пустой пароль должен возвращать ошибку
	if _, err := DeriveKey(""); err == nil {
		t.Fatalf("DeriveKey with empty password should return error")
	}
}

func TestKeyID(t *testing.T) {
	id1 := KeyID("testpass")
	id2 := KeyID("testpass")
	if id1 != id2 {
		t.Fatalf("KeyID should be deterministic")
	}
	if len(id1) != 16 {
		t.Fatalf("KeyID should be 16 hex chars (8 bytes), got %d", len(id1))
	}
}

func TestBuildNonce(t *testing.T) {
	ssrc := uint32(0x12345678)
	seq := uint16(0x9ABC)
	ts := uint32(0xDEF01234)

	nonce := BuildNonce(ssrc, seq, ts)
	if len(nonce) != NonceLen {
		t.Fatalf("Nonce length must be %d", NonceLen)
	}

	// Проверяем байты
	if nonce[0] != 0x12 || nonce[1] != 0x34 || nonce[2] != 0x56 || nonce[3] != 0x78 {
		t.Errorf("SSRC part of nonce incorrect: %x", nonce[0:4])
	}
	if nonce[4] != 0x9A || nonce[5] != 0xBC {
		t.Errorf("Seq part of nonce incorrect: %x", nonce[4:6])
	}
	if nonce[6] != 0x00 || nonce[7] != 0x00 {
		t.Errorf("Reserved part of nonce must be zero: %x", nonce[6:8])
	}
	if nonce[8] != 0xDE || nonce[9] != 0xF0 || nonce[10] != 0x12 || nonce[11] != 0x34 {
		t.Errorf("Timestamp part of nonce incorrect: %x", nonce[8:12])
	}
}

func TestGetAEAD(t *testing.T) {
	key, _ := DeriveKey("password")
	aead1, err := GetAEAD(key)
	if err != nil {
		t.Fatalf("GetAEAD failed: %v", err)
	}

	aead2, err := GetAEAD(key)
	if err != nil {
		t.Fatalf("GetAEAD failed second time: %v", err)
	}
	if aead1 != aead2 {
		t.Errorf("GetAEAD should return cached instance")
	}

	// Проверяем невалидный размер ключа
	if _, err := GetAEAD([]byte("short")); err == nil {
		t.Errorf("GetAEAD with short key should return error")
	}
}
