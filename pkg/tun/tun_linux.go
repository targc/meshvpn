//go:build linux

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
	tunDevice = "/dev/net/tun"
	ifnamsiz  = 16
	iffTun    = 0x0001
	iffNoPi   = 0x1000
)

type ifReq struct {
	Name  [ifnamsiz]byte
	Flags uint16
	_     [22]byte
}

type Device struct {
	file *os.File
	name string
	ip   net.IP
	mask net.IPMask
}

func New(name string, ip net.IP, mask net.IPMask) (*Device, error) {
	fd, err := syscall.Open(tunDevice, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", tunDevice, err)
	}

	var req ifReq
	copy(req.Name[:], name)
	req.Flags = iffTun | iffNoPi

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("ioctl TUNSETIFF failed: %v", errno)
	}

	actualName := string(req.Name[:])
	for i, b := range req.Name {
		if b == 0 {
			actualName = string(req.Name[:i])
			break
		}
	}

	dev := &Device{
		file: os.NewFile(uintptr(fd), tunDevice),
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

	if err := exec.Command("ip", "addr", "add", ipStr, "dev", d.name).Run(); err != nil {
		return fmt.Errorf("failed to set IP: %w", err)
	}

	if err := exec.Command("ip", "link", "set", d.name, "up").Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	if err := exec.Command("ip", "link", "set", d.name, "mtu", "1400").Run(); err != nil {
		return fmt.Errorf("failed to set MTU: %w", err)
	}

	return nil
}

func (d *Device) Read(buf []byte) (int, error) {
	return d.file.Read(buf)
}

func (d *Device) Write(buf []byte) (int, error) {
	return d.file.Write(buf)
}

func (d *Device) Close() error {
	return d.file.Close()
}

func (d *Device) Name() string {
	return d.name
}
