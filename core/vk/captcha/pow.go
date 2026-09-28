package captcha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
)

var ErrPoWTimeout = errors.New("captcha: PoW solution cancelled or timed out")

// SolvePoW многопоточно находит hex-хеш SHA-256(powInput + nonce), начинающийся с difficulty нулей.
// Использование всех доступных ядер процессора сокращает время поиска до сотых долей секунды.
func SolvePoW(ctx context.Context, powInput string, difficulty int) (string, error) {
	if difficulty <= 0 {
		difficulty = 1
	}
	targetPrefix := strings.Repeat("0", difficulty)

	numWorkers := runtime.NumCPU()
	if numWorkers <= 0 {
		numWorkers = 4
	}

	resultCh := make(chan string, 1)
	var found atomic.Bool
	step := uint64(1000)

	for w := 0; w < numWorkers; w++ {
		startNonce := uint64(w) * step
		go func(start uint64) {
			currNonce := start
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if found.Load() {
					return
				}

				for i := uint64(0); i < step; i++ {
					nonceStr := strconv.FormatUint(currNonce+i, 10)
					sum := sha256.Sum256([]byte(powInput + nonceStr))
					hexStr := hex.EncodeToString(sum[:])

					if strings.HasPrefix(hexStr, targetPrefix) {
						if found.CompareAndSwap(false, true) {
							select {
							case resultCh <- hexStr:
							default:
							}
						}
						return
					}
				}

				currNonce += uint64(numWorkers) * step
				// Ограничение диапазона поиска 10 миллионами
				if currNonce > 10000000 {
					return
				}
			}
		}(startNonce)
	}

	select {
	case <-ctx.Done():
		return "", ErrPoWTimeout
	case res := <-resultCh:
		return res, nil
	}
}
