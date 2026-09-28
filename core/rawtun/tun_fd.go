package rawtun

import (
	"errors"
	"fmt"
	"os"
)

var ErrInvalidFd = errors.New("tun: invalid file descriptor")

// FdTun оборачивает дескриптор виртуального интерфейса (например, созданного Android VpnService).
type FdTun struct {
	file *os.File
	name string
	mtu  int
}

// WrapFd создает TunDevice из системного файлового дескриптора.
func WrapFd(fd int, mtu int) (*FdTun, error) {
	if fd < 0 {
		return nil, ErrInvalidFd
	}
	if mtu <= 0 {
		mtu = 1280
	}

	file := os.NewFile(uintptr(fd), "tun")
	if file == nil {
		return nil, fmt.Errorf("tun: failed to wrap fd %d into os.File", fd)
	}

	return &FdTun{
		file: file,
		name: "fdtun",
		mtu:  mtu,
	}, nil
}

func (t *FdTun) Read(b []byte) (int, error) {
	return t.file.Read(b)
}

func (t *FdTun) Write(b []byte) (int, error) {
	return t.file.Write(b)
}

func (t *FdTun) Close() error {
	return t.file.Close()
}

func (t *FdTun) MTU() int {
	return t.mtu
}

func (t *FdTun) Name() string {
	return t.name
}
