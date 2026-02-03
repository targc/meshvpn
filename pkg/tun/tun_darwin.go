//go:build darwin

package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	utunControlName = "com.apple.net.utun_control"
	utunOptIfname   = 2
)

type ctlInfo struct {
	ctlID   uint32
	ctlName [96]byte
}

type sockaddrCtl struct {
	scLen      uint8
	scFamily   uint8
	ssSysaddr  uint16
	scID       uint32
	scUnit     uint32
	scReserved [5]uint32
}

type Device struct {
	file *os.File
	name string
	ip   net.IP
	mask net.IPMask
}

func New(name string, ip net.IP, mask net.IPMask) (*Device, error) {
	fd, err := syscall.Socket(syscall.AF_SYSTEM, syscall.SOCK_DGRAM, 2)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %w", err)
	}

	var info ctlInfo
	copy(info.ctlName[:], utunControlName)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0xc0644e03), uintptr(unsafe.Pointer(&info)))
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("ioctl CTLIOCGINFO failed: %v", errno)
	}

	addr := sockaddrCtl{
		scLen:     uint8(unsafe.Sizeof(sockaddrCtl{})),
		scFamily:  syscall.AF_SYSTEM,
		ssSysaddr: 2,
		scID:      info.ctlID,
		scUnit:    0,
	}

	_, _, errno = syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&addr)), unsafe.Sizeof(addr))
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("connect failed: %v", errno)
	}

	ifname := make([]byte, 16)
	ifnameLen := uint32(len(ifname))
	_, _, errno = syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(fd), 2, utunOptIfname, uintptr(unsafe.Pointer(&ifname[0])), uintptr(unsafe.Pointer(&ifnameLen)), 0)
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("getsockopt failed: %v", errno)
	}

	actualName := string(ifname[:ifnameLen-1])

	dev := &Device{
		file: os.NewFile(uintptr(fd), "/dev/utun"),
		name: actualName,
		ip:   ip,
		mask: mask,
	}

	if err := dev.configure(); err != nil {
		dev.Close()
		return nil, err
	}

	return dev, nil
}

func (d *Device) configure() error {
	cidr, _ := d.mask.Size()
	ipStr := fmt.Sprintf("%s/%d", d.ip.String(), cidr)

	if err := exec.Command("ifconfig", d.name, "inet", d.ip.String(), d.ip.String(), "up").Run(); err != nil {
		return fmt.Errorf("failed to configure interface: %w", err)
	}

	_, network, _ := net.ParseCIDR(ipStr)
	if err := exec.Command("route", "add", "-net", network.String(), "-interface", d.name).Run(); err != nil {
		return fmt.Errorf("failed to add route: %w", err)
	}

	if err := exec.Command("ifconfig", d.name, "mtu", "1400").Run(); err != nil {
		return fmt.Errorf("failed to set MTU: %w", err)
	}

	return nil
}

func (d *Device) Read(buf []byte) (int, error) {
	// macOS utun prepends 4-byte header
	tmp := make([]byte, len(buf)+4)
	n, err := d.file.Read(tmp)
	if err != nil {
		return 0, err
	}
	if n <= 4 {
		return 0, nil
	}
	copy(buf, tmp[4:n])
	return n - 4, nil
}

func (d *Device) Write(buf []byte) (int, error) {
	// macOS utun needs 4-byte header (AF_INET = 2)
	tmp := make([]byte, len(buf)+4)
	tmp[3] = 2 // AF_INET
	copy(tmp[4:], buf)
	n, err := d.file.Write(tmp)
	if err != nil {
		return 0, err
	}
	return n - 4, nil
}

func (d *Device) Close() error {
	return d.file.Close()
}

func (d *Device) Name() string {
	return d.name
}
