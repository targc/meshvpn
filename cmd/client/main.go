package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tar/s2s/pkg/crypto"
	"github.com/tar/s2s/pkg/protocol"
	"github.com/tar/s2s/pkg/tun"
)

func main() {
	relayAddr := flag.String("relay", "", "relay server address (e.g., 1.2.3.4:51820)")
	key := flag.String("key", "", "pre-shared key")
	ipStr := flag.String("ip", "", "virtual IP with CIDR (e.g., 10.99.0.1/24)")
	tunName := flag.String("tun", "tun0", "TUN device name")
	flag.Parse()

	if *relayAddr == "" || *key == "" || *ipStr == "" {
		log.Fatal("--relay, --key, and --ip are required")
	}

	ip, ipNet, err := net.ParseCIDR(*ipStr)
	if err != nil {
		log.Fatalf("invalid IP: %v", err)
	}

	cipher, err := crypto.NewCipher(*key)
	if err != nil {
		log.Fatalf("failed to create cipher: %v", err)
	}

	relay, err := net.ResolveUDPAddr("udp", *relayAddr)
	if err != nil {
		log.Fatalf("failed to resolve relay: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		log.Fatalf("failed to connect to relay: %v", err)
	}
	defer conn.Close()

	dev, err := tun.New(*tunName, ip, ipNet.Mask)
	if err != nil {
		log.Fatalf("failed to create TUN: %v", err)
	}
	defer dev.Close()

	log.Printf("TUN device %s created with IP %s", dev.Name(), ip)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		cancel()
	}()

	client := &Client{
		conn:   conn,
		cipher: cipher,
		dev:    dev,
		ip:     ip,
	}

	client.Run(ctx)
}

type Client struct {
	conn   *net.UDPConn
	cipher *crypto.Cipher
	dev    *tun.Device
	ip     net.IP
}

func (c *Client) Run(ctx context.Context) {
	go c.register(ctx)
	go c.readFromTUN(ctx)
	go c.readFromRelay(ctx)

	<-ctx.Done()
}

func (c *Client) register(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	send := func() {
		pkt := protocol.NewRegisterPacket(c.ip)
		encrypted, err := c.cipher.Encrypt(pkt.Encode())
		if err != nil {
			log.Printf("encrypt error: %v", err)
			return
		}
		c.conn.Write(encrypted)
	}

	send() // initial registration
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func (c *Client) readFromTUN(ctx context.Context) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := c.dev.Read(buf)
		if err != nil {
			log.Printf("TUN read error: %v", err)
			continue
		}

		pkt := protocol.NewDataPacket(buf[:n])
		encrypted, err := c.cipher.Encrypt(pkt.Encode())
		if err != nil {
			log.Printf("encrypt error: %v", err)
			continue
		}

		c.conn.Write(encrypted)
	}
}

func (c *Client) readFromRelay(ctx context.Context) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := c.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("relay read error: %v", err)
			continue
		}

		decrypted, err := c.cipher.Decrypt(buf[:n])
		if err != nil {
			log.Printf("decrypt error: %v", err)
			continue
		}

		pkt, err := protocol.Decode(decrypted)
		if err != nil {
			log.Printf("decode error: %v", err)
			continue
		}

		if pkt.Type == protocol.PacketData {
			c.dev.Write(pkt.Payload)
		}
	}
}
