package protocol

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// RAMagic0 первый байт магической сигнатуры RA-кадра ('R').
	RAMagic0 = 'R'

	// RAMagic1 второй байт магической сигнатуры RA-кадра ('A').
	RAMagic1 = 'A'

	// RAHeaderLen длина внутреннего заголовка мультиплексирования (2 байта Magic + 4 байта Seq).
	RAHeaderLen = 6

	// ReorderMaxCapacity максимальное число пакетов в буфере переупорядочивания.
	ReorderMaxCapacity = 2048

	// ReorderStallTTL таймаут ожидания пропущенного пакета (40 мс).
	ReorderStallTTL = 40 * time.Millisecond
)

// EncodeRAFrame оборачивает полезную нагрузку (IP-пакет) в заголовок RA Frame с порядковым номером.
func EncodeRAFrame(seq uint32, ipPayload []byte) []byte {
	out := make([]byte, RAHeaderLen+len(ipPayload))
	out[0] = RAMagic0
	out[1] = RAMagic1
	binary.BigEndian.PutUint32(out[2:6], seq)
	copy(out[RAHeaderLen:], ipPayload)
	return out
}

// DecodeRAFrame извлекает порядковый номер и IP-пакет из кадра RA Frame.
// Возвращает ok=false, если сигнатура не совпадает или пакет не похож на IPv4.
func DecodeRAFrame(pkt []byte) (seq uint32, ipPayload []byte, ok bool) {
	if len(pkt) < RAHeaderLen+20 { // Минимальный IPv4 заголовок - 20 байт
		return 0, nil, false
	}
	if pkt[0] != RAMagic0 || pkt[1] != RAMagic1 {
		return 0, nil, false
	}
	seq = binary.BigEndian.Uint32(pkt[2:6])
	ipPayload = pkt[RAHeaderLen:]

	// Проверяем, что первый полубайт полезной нагрузки равен 4 (IPv4)
	if ipPayload[0]>>4 != 4 {
		return 0, nil, false
	}
	return seq, ipPayload, true
}

// IsRAFrame проверяет, начинается ли датаграмма с сигнатуры RA Frame.
func IsRAFrame(pkt []byte) bool {
	return len(pkt) >= RAHeaderLen && pkt[0] == RAMagic0 && pkt[1] == RAMagic1
}

// OutSeq потокобезопасный монотонно возрастающий генератор номеров последовательности.
type OutSeq struct {
	val atomic.Uint32
}

// Next возвращает следующий инкрементный seq.
func (s *OutSeq) Next() uint32 {
	return s.val.Add(1) - 1
}

// ReorderBuffer кольцевой буфер упорядочивания пакетов, устраняющий джиттер параллельных воркеров.
type ReorderBuffer struct {
	mu        sync.Mutex
	next      uint32
	inited    bool
	buf       map[uint32][]byte
	waitSince time.Time
}

// NewReorderBuffer создает новый буфер переупорядочивания.
func NewReorderBuffer() *ReorderBuffer {
	return &ReorderBuffer{
		buf: make(map[uint32][]byte),
	}
}

// Push добавляет принятый пакет в буфер и возвращает непрерывную упорядоченную последовательность готовых пакетов.
func (r *ReorderBuffer) Push(seq uint32, ipPayload []byte) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.inited {
		r.next = seq
		r.inited = true
	}

	// Отбрасываем старые дубликаты (с учетом кольцевого переполнения uint32)
	if seq < r.next && (r.next-seq) < 0x80000000 {
		return nil
	}

	// Если пакет с таким номером уже есть в буфере - игнорируем дубликат
	if _, exists := r.buf[seq]; exists {
		return nil
	}

	// Защита от переполнения: если буфер превысил лимит, сбрасываем окно вперед
	if len(r.buf) >= ReorderMaxCapacity {
		r.next = seq
		r.buf = make(map[uint32][]byte)
		r.waitSince = time.Time{}
	}

	// Сохраняем копию полезной нагрузки
	r.buf[seq] = append([]byte(nil), ipPayload...)

	var ready [][]byte
	for {
		// Если ожидаемый следующий пакет в наличии - извлекаем
		if p, ok := r.buf[r.next]; ok {
			ready = append(ready, p)
			delete(r.buf, r.next)
			r.next++
			r.waitSince = time.Time{}
			continue
		}

		// Буфер пуст - ждать нечего
		if len(r.buf) == 0 {
			r.waitSince = time.Time{}
			break
		}

		now := time.Now()
		if r.waitSince.IsZero() {
			r.waitSince = now
			break
		}

		// Если мы ждем пропущенный пакет дольше StallTTL - форсируем сдвиг окна к минимальному seq,
		// чтобы не вешать TCP-соединения
		if now.Sub(r.waitSince) >= ReorderStallTTL {
			var minSeq uint32
			first := true
			for s := range r.buf {
				if first || s < minSeq {
					minSeq = s
					first = false
				}
			}
			r.next = minSeq
			r.waitSince = time.Time{}
			continue
		}

		// Таймаут еще не истек, продолжаем ждать пропущенный пакет
		break
	}

	return ready
}
