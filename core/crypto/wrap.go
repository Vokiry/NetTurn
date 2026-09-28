package crypto

import (
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	// KeyLen длина симметричного ключа ChaCha20-Poly1305 в байтах.
	KeyLen = 32

	// NonceLen длина Nonce для ChaCha20-Poly1305 в байтах.
	NonceLen = 12

	// SaltHKDF стандартная соль для протокола WRAP.
	SaltHKDF = "WDTT-WRAP-v1"

	// InfoHKDF строка контекста HKDF для RTP-обфускации.
	InfoHKDF = "rtp-obfs/chacha20poly1305"

	// KeyIDPrefix префикс хэша идентификатора ключа.
	KeyIDPrefix = "WDTT-WRAP-ID-v1\x00"
)

var (
	ErrEmptyPassword = errors.New("crypto: empty password")
	ErrInvalidKeyLen = errors.New("crypto: key must be 32 bytes")

	// Кэш AEAD шифров для избежания лишних аллокаций.
	aeadCache sync.Map // string(key) -> cipher.AEAD
)

// DeriveKey вычисляет 32-байтный сессионный ключ из пароля через HKDF-SHA256.
func DeriveKey(password string) ([]byte, error) {
	if password == "" {
		return nil, ErrEmptyPassword
	}
	key := make([]byte, KeyLen)
	reader := hkdf.New(
		sha256.New,
		[]byte(password),
		[]byte(SaltHKDF),
		[]byte(InfoHKDF),
	)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("crypto: hkdf derivation failed: %w", err)
	}
	return key, nil
}

// KeyID генерирует короткий 16-символьный hex-идентификатор ключа для быстрой проверки паролей на сервере.
func KeyID(password string) string {
	sum := sha256.Sum256([]byte(KeyIDPrefix + password))
	return hex.EncodeToString(sum[:8])
}

// BuildNonce формирует 12-байтный Nonce для ChaCha20-Poly1305 из параметров RTP-пакета:
// Nonce[0..3] = SSRC, Nonce[4..5] = Seq, Nonce[6..7] = 0, Nonce[8..11] = Timestamp.
func BuildNonce(ssrc uint32, seq uint16, ts uint32) [NonceLen]byte {
	var nonce [NonceLen]byte
	binary.BigEndian.PutUint32(nonce[0:4], ssrc)
	binary.BigEndian.PutUint16(nonce[4:6], seq)
	nonce[6] = 0x00
	nonce[7] = 0x00
	binary.BigEndian.PutUint32(nonce[8:12], ts)
	return nonce
}

// GetAEAD возвращает инициализированный экземпляр ChaCha20-Poly1305 для заданного 32-байтного ключа.
func GetAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLen {
		return nil, ErrInvalidKeyLen
	}
	keyStr := string(key)
	if cached, ok := aeadCache.Load(keyStr); ok {
		return cached.(cipher.AEAD), nil
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new chacha20poly1305: %w", err)
	}
	aeadCache.Store(keyStr, aead)
	return aead, nil
}

// ZeroBytes безопасно затирает слайс байт нулями в памяти.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
