package relay

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/tar/meshvpn/pkg/crypto"
	"github.com/tar/meshvpn/pkg/protocol"
)

type Node struct {
	VirtualIP net.IP
	Endpoint  *net.UDPAddr
	LastSeen  time.Time
}

type Server struct {
	conn     *net.UDPConn
	cipher   *crypto.Cipher
	nodes    map[string]*Node // virtual IP string -> Node
	mu       sync.RWMutex
	timeout  time.Duration
}

func NewServer(cipher *crypto.Cipher) *Server {
	return &Server{
		cipher:  cipher,
		nodes:   make(map[string]*Node),
		timeout: 30 * time.Second,
	}
}

func (s *Server) Run(ctx context.Context, addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.conn = conn
	defer conn.Close()

	log.Printf("relay server listening on %s", addr)

	go s.cleanupLoop(ctx)

	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("read error: %v", err)
			continue
		}

		s.handlePacket(buf[:n], remoteAddr)
	}
}

func (s *Server) handlePacket(encrypted []byte, from *net.UDPAddr) {
	decrypted, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		log.Printf("decrypt error from %s: %v", from, err)
		return
	}

	pkt, err := protocol.Decode(decrypted)
	if err != nil {
		log.Printf("decode error: %v", err)
		return
	}

	switch pkt.Type {
	case protocol.PacketRegister:
		s.handleRegister(pkt.Payload, from)
	case protocol.PacketKeepalive:
		s.handleKeepalive(from)
	case protocol.PacketData:
		s.handleData(pkt.Payload, from)
	}
}

func (s *Server) handleRegister(payload []byte, from *net.UDPAddr) {
	virtualIP, err := protocol.ParseRegister(payload)
	if err != nil {
		log.Printf("invalid register: %v", err)
		return
	}

	s.mu.Lock()
	s.nodes[virtualIP.String()] = &Node{
		VirtualIP: virtualIP,
		Endpoint:  from,
		LastSeen:  time.Now(),
	}
	s.mu.Unlock()

	log.Printf("registered node %s from %s", virtualIP, from)
}

func (s *Server) handleKeepalive(from *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, node := range s.nodes {
		if node.Endpoint.String() == from.String() {
			node.LastSeen = time.Now()
			node.Endpoint = from // update in case NAT port changed
			return
		}
	}
}

func (s *Server) handleData(ipPacket []byte, from *net.UDPAddr) {
	destIP, err := protocol.GetDestIP(ipPacket)
	if err != nil {
		log.Printf("get dest IP error: %v", err)
		return
	}

	s.mu.RLock()
	destNode, ok := s.nodes[destIP.String()]
	s.mu.RUnlock()

	if !ok {
		return // destination not registered, drop
	}

	pkt := protocol.NewDataPacket(ipPacket)
	encrypted, err := s.cipher.Encrypt(pkt.Encode())
	if err != nil {
		log.Printf("encrypt error: %v", err)
		return
	}

	_, err = s.conn.WriteToUDP(encrypted, destNode.Endpoint)
	if err != nil {
		log.Printf("send error: %v", err)
	}
}

func (s *Server) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *Server) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for ip, node := range s.nodes {
		if now.Sub(node.LastSeen) > s.timeout {
			log.Printf("removing stale node %s", ip)
			delete(s.nodes, ip)
		}
	}
}
