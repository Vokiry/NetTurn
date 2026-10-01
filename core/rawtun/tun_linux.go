//go:build linux && !android

package rawtun

import (
	"fmt"
	"net"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// LinuxTun реализует интерфейс TunDevice для Linux через /dev/net/tun.
type LinuxTun struct {
	file *os.File
	name string
	mtu  int
}

type ifreqAddr struct {
	ifrName [unix.IFNAMSIZ]byte
	ifrAddr unix.RawSockaddrInet4
}

type ifreqFlags struct {
	ifrName  [unix.IFNAMSIZ]byte
	ifrFlags uint16
}

type ifreqMTU struct {
	ifrName [unix.IFNAMSIZ]byte
	ifrMTU  int32
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

// Configure настраивает IP адрес, маску /16, MTU и переводит интерфейс в состояние UP через прямые ioctl системные вызовы.
func (t *LinuxTun) Configure(ipStr string) error {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("tun: socket: %w", err)
	}
	defer unix.Close(sock)

	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return fmt.Errorf("tun: invalid IP address: %s", ipStr)
	}

	// 1. Установка IP адреса
	var reqAddr ifreqAddr
	copy(reqAddr.ifrName[:], t.name)
	reqAddr.ifrAddr.Family = unix.AF_INET
	copy(reqAddr.ifrAddr.Addr[:], ip)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sock), uintptr(unix.SIOCSIFADDR), uintptr(unsafe.Pointer(&reqAddr))); errno != 0 {
		return fmt.Errorf("tun: SIOCSIFADDR: %v", errno)
	}

	// 2. Установка маски подсети 255.255.0.0 (/16)
	var reqMask ifreqAddr
	copy(reqMask.ifrName[:], t.name)
	reqMask.ifrAddr.Family = unix.AF_INET
	copy(reqMask.ifrAddr.Addr[:], []byte{255, 255, 0, 0})
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sock), uintptr(unix.SIOCSIFNETMASK), uintptr(unsafe.Pointer(&reqMask))); errno != 0 {
		return fmt.Errorf("tun: SIOCSIFNETMASK: %v", errno)
	}

	// 3. Установка MTU
	var reqMTU ifreqMTU
	copy(reqMTU.ifrName[:], t.name)
	reqMTU.ifrMTU = int32(t.mtu)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sock), uintptr(unix.SIOCSIFMTU), uintptr(unsafe.Pointer(&reqMTU))); errno != 0 {
		return fmt.Errorf("tun: SIOCSIFMTU: %v", errno)
	}

	// 4. Перевод интерфейса в состояние UP и RUNNING
	var reqFlags ifreqFlags
	copy(reqFlags.ifrName[:], t.name)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sock), uintptr(unix.SIOCGIFFLAGS), uintptr(unsafe.Pointer(&reqFlags))); errno != 0 {
		return fmt.Errorf("tun: SIOCGIFFLAGS: %v", errno)
	}

	reqFlags.ifrFlags |= unix.IFF_UP | unix.IFF_RUNNING
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(sock), uintptr(unix.SIOCSIFFLAGS), uintptr(unsafe.Pointer(&reqFlags))); errno != 0 {
		return fmt.Errorf("tun: SIOCSIFFLAGS: %v", errno)
	}

	return nil
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
