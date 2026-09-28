//go:build linux && !android

package rawtun

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// LinuxTun реализует интерфейс TunDevice для Linux через /dev/net/tun.
type LinuxTun struct {
	file *os.File
	name string
	mtu  int
}

// CreateLinuxTun создает и регистрирует TUN-адаптер в ядре Linux.
func CreateLinuxTun(name string, mtu int) (*LinuxTun, error) {
	if mtu <= 0 {
		mtu = 1280
	}
	if name == "" {
		name = "netturn0"
	}

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("tun: open /dev/net/tun failed: %w", err)
	}

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("tun: new ifreq failed: %w", err)
	}

	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("tun: ioctl TUNSETIFF failed: %w", err)
	}

	if err := unix.SetNonblock(fd, false); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("tun: set nonblock failed: %w", err)
	}

	file := os.NewFile(uintptr(fd), "/dev/net/tun")
	return &LinuxTun{
		file: file,
		name: ifr.Name(),
		mtu:  mtu,
	}, nil
}

func (t *LinuxTun) Read(b []byte) (int, error) {
	return t.file.Read(b)
}

func (t *LinuxTun) Write(b []byte) (int, error) {
	return t.file.Write(b)
}

func (t *LinuxTun) Close() error {
	return t.file.Close()
}

func (t *LinuxTun) MTU() int {
	return t.mtu
}

func (t *LinuxTun) Name() string {
	return t.name
}
