package protocol

import (
	"encoding/binary"
	"fmt"
	"net"
)

type PacketType byte

const (
	PacketRegister  PacketType = 0x01
	PacketKeepalive PacketType = 0x02
	PacketData      PacketType = 0x03
)

type Packet struct {
	Type    PacketType
	Payload []byte
}

func (p *Packet) Encode() []byte {
	buf := make([]byte, 1+len(p.Payload))
	buf[0] = byte(p.Type)
	copy(buf[1:], p.Payload)
	return buf
}

func Decode(data []byte) (*Packet, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("packet too short")
	}

	return &Packet{
		Type:    PacketType(data[0]),
		Payload: data[1:],
	}, nil
}

func NewRegisterPacket(virtualIP net.IP) *Packet {
	ip := virtualIP.To4()
	if ip == nil {
		ip = virtualIP
	}
	return &Packet{
		Type:    PacketRegister,
		Payload: ip,
	}
}

func NewKeepalivePacket() *Packet {
	return &Packet{
		Type:    PacketKeepalive,
		Payload: nil,
	}
}

func NewDataPacket(data []byte) *Packet {
	return &Packet{
		Type:    PacketData,
		Payload: data,
	}
}

func ParseRegister(payload []byte) (net.IP, error) {
	if len(payload) != 4 && len(payload) != 16 {
		return nil, fmt.Errorf("invalid IP length: %d", len(payload))
	}
	return net.IP(payload), nil
}

func GetDestIP(ipPacket []byte) (net.IP, error) {
	if len(ipPacket) < 20 {
		return nil, fmt.Errorf("IP packet too short")
	}

	version := ipPacket[0] >> 4
	if version == 4 {
		return net.IP(ipPacket[16:20]), nil
	} else if version == 6 {
		if len(ipPacket) < 40 {
			return nil, fmt.Errorf("IPv6 packet too short")
		}
		return net.IP(ipPacket[24:40]), nil
	}

	return nil, fmt.Errorf("unknown IP version: %d", version)
}

func GetSrcIP(ipPacket []byte) (net.IP, error) {
	if len(ipPacket) < 20 {
		return nil, fmt.Errorf("IP packet too short")
	}

	version := ipPacket[0] >> 4
	if version == 4 {
		return net.IP(ipPacket[12:16]), nil
	} else if version == 6 {
		if len(ipPacket) < 40 {
			return nil, fmt.Errorf("IPv6 packet too short")
		}
		return net.IP(ipPacket[8:24]), nil
	}

	return nil, fmt.Errorf("unknown IP version: %d", version)
}

var _ = binary.BigEndian // silence import
