package rawtun

import (
	"io"
)

// TunDevice абстрагирует виртуальный сетевой интерфейс для кроссплатформенной работы.
type TunDevice interface {
	io.ReadWriteCloser
	MTU() int
	Name() string
}
