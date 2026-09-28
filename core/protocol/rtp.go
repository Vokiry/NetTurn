package protocol

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/Vokiry/NetTurn/core"
	"github.com/Vokiry/NetTurn/core/crypto"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// RTPHeaderLen фиксированная длина заголовка RTP v2 без расширений и CSRC.
	RTPHeaderLen = 12

	// RTPVersion2 версия протокола RTP (2).
	RTPVersion2 = 2

	// PayloadTypeAudio Opus Audio (111).
	PayloadTypeAudio = 111

	// PayloadTypeVideo VP8 Video (96).
	PayloadTypeVideo = 96

	// PaddingMaxAudio максимальный размер случайного паддинга для аудио.
	PaddingMaxAudio = 24

	// PaddingMaxVideo максимальный размер случайного паддинга для видео.
	PaddingMaxVideo = 60
)

var (
	ErrPacketTooShort   = errors.New("rtp: packet too short (less than 13 bytes)")
	ErrNotRTPv2         = errors.New("rtp: packet is not valid RTP v2")
	ErrInvalidPadding   = errors.New("rtp: invalid padding length")
	ErrEmptyPayload     = errors.New("rtp: empty payload")
	ErrBufferTooSmall   = errors.New("rtp: destination buffer too small")
	ErrAuthFailed       = errors.New("rtp: authentication/decryption failed")
)

// RTPConfig настройки RTP-обфускации для воркера.
type RTPConfig struct {
	SSRC        uint32
	PayloadType uint8
	PaddingMax  int
}

// NewRTPConfig создает конфигурацию со случайным SSRC и параметрами в зависимости от типа обфускации.
func NewRTPConfig(obfs core.ObfsType) *RTPConfig {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	ssrc := binary.BigEndian.Uint32(buf[:])

	pt := uint8(PayloadTypeAudio)
	padMax := PaddingMaxAudio
	if obfs == core.ObfsVideo {
		pt = PayloadTypeVideo
		padMax = PaddingMaxVideo
	}

	return &RTPConfig{
		SSRC:        ssrc,
		PayloadType: pt,
		PaddingMax:  padMax,
	}
}

// RTPState отслеживает монотонный счетчик пакетов и временные метки RTP.
type RTPState struct {
	mu      sync.Mutex
	initSeq uint16
	initTs  uint32
	count   uint64
}

// NewRTPState инициализирует начальные случайные Sequence Number и Timestamp.
func NewRTPState() *RTPState {
	var buf [6]byte
	_, _ = rand.Read(buf[:])
	return &RTPState{
		initSeq: binary.BigEndian.Uint16(buf[0:2]),
		initTs:  binary.BigEndian.Uint32(buf[2:6]),
		count:   0,
	}
}

// WrapPacket упаковывает сырой IP-пакет в зашифрованный RTP v2 контейнер с добавлением случайного паддинга.
func WrapPacket(key []byte, payload []byte, cfg *RTPConfig, state *RTPState) ([]byte, error) {
	if len(key) != crypto.KeyLen {
		return nil, crypto.ErrInvalidKeyLen
	}
	if len(payload) == 0 {
		return nil, ErrEmptyPayload
	}

	state.mu.Lock()
	c := state.count
	state.count++
	state.mu.Unlock()

	seq := state.initSeq + uint16(c)
	// Шаг таймстемпа 960 семплов на кадр (как в Opus 20ms) + микро-смещение старших бит
	ts := state.initTs + uint32(c)*960 + uint32(c>>16)

	nonce := crypto.BuildNonce(cfg.SSRC, seq, ts)

	padRand := 0
	if cfg.PaddingMax > 0 {
		var rndBuf [1]byte
		_, _ = rand.Read(rndBuf[:])
		padRand = int(rndBuf[0]) % cfg.PaddingMax
	}
	padTotal := padRand + 1

	outLen := RTPHeaderLen + len(payload) + chacha20poly1305.Overhead + padTotal
	out := make([]byte, outLen)

	// RTP Header
	out[0] = 0x80 | 0x20 // V=2, P=1 (Padding bit set)
	out[1] = cfg.PayloadType & 0x7F
	binary.BigEndian.PutUint16(out[2:4], seq)
	binary.BigEndian.PutUint32(out[4:8], ts)
	binary.BigEndian.PutUint32(out[8:12], cfg.SSRC)

	aead, err := crypto.GetAEAD(key)
	if err != nil {
		return nil, fmt.Errorf("rtp: cipher init: %w", err)
	}

	// Шифрование полезного груза. AAD = первые 12 байт заголовка RTP
	sealed := aead.Seal(out[RTPHeaderLen:RTPHeaderLen], nonce[:], payload, out[:RTPHeaderLen])

	// Заполнение паддинга
	padStart := RTPHeaderLen + len(sealed)
	if padRand > 0 {
		_, _ = rand.Read(out[padStart : padStart+padRand])
	}
	// Последний байт пакета - общая длина паддинга
	out[outLen-1] = byte(padTotal)

	return out, nil
}

// UnwrapPacket проверяет валидность RTP контейнера, удаляет паддинг и дешифрует полезную нагрузку в dst.
// Возвращает количество расшифрованных байт.
func UnwrapPacket(key []byte, wire []byte, dst []byte) (int, error) {
	if len(key) != crypto.KeyLen {
		return 0, crypto.ErrInvalidKeyLen
	}
	if len(wire) < RTPHeaderLen+1 {
		return 0, ErrPacketTooShort
	}

	// Проверка версии RTP (первые 2 бита первого байта должны быть равны 2)
	if (wire[0] >> 6) != RTPVersion2 {
		return 0, ErrNotRTPv2
	}

	seq := binary.BigEndian.Uint16(wire[2:4])
	ts := binary.BigEndian.Uint32(wire[4:8])
	ssrc := binary.BigEndian.Uint32(wire[8:12])

	payloadEnd := len(wire)

	// Обработка бита Padding (бит 5 первого байта: 0x20)
	if wire[0]&0x20 != 0 {
		padLen := int(wire[len(wire)-1])
		if padLen == 0 || padLen > payloadEnd-RTPHeaderLen {
			return 0, fmt.Errorf("%w: padLen=%d, total=%d", ErrInvalidPadding, padLen, len(wire))
		}
		payloadEnd -= padLen
	}

	ciphertextLen := payloadEnd - RTPHeaderLen
	if ciphertextLen <= chacha20poly1305.Overhead {
		return 0, ErrEmptyPayload
	}

	plainLen := ciphertextLen - chacha20poly1305.Overhead
	if len(dst) < plainLen {
		return 0, ErrBufferTooSmall
	}

	nonce := crypto.BuildNonce(ssrc, seq, ts)
	aead, err := crypto.GetAEAD(key)
	if err != nil {
		return 0, fmt.Errorf("rtp: cipher init: %w", err)
	}

	plain, err := aead.Open(dst[:0], nonce[:], wire[RTPHeaderLen:payloadEnd], wire[:RTPHeaderLen])
	if err != nil {
		return 0, ErrAuthFailed
	}

	return len(plain), nil
}

// IsRTPPacket проверяет по формату заголовка, является ли пакет RTP v2 аудио или видео пакетом.
func IsRTPPacket(wire []byte) bool {
	if len(wire) < RTPHeaderLen+1 {
		return false
	}
	if (wire[0] >> 6) != RTPVersion2 {
		return false
	}
	pt := wire[1] & 0x7F
	return pt == PayloadTypeAudio || pt == PayloadTypeVideo
}
