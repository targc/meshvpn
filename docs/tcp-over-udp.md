# How TCP Works Over UDP Tunnel

## The Question

> "Why can this use UDP, while I send data via TCP?"

## The Answer

The VPN **wraps** your entire TCP packet inside UDP. Your TCP is preserved, untouched.

```
App sees:     TCP connection to 10.55.0.2:6443
Network sees: UDP packets to relay:51820
```

## Full Packet Flow

```
┌──────────────────────────────────────────────────────────────┐
│  APP: curl https://10.55.0.2:6443                            │
│       ↓                                                      │
│  Kernel creates TCP packet, routes to tun0                   │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  readFromTUN() - cmd/client/main.go:119                      │
│                                                              │
│  n, err := c.dev.Read(buf)  // reads FULL IP packet          │
│                             // including TCP headers         │
│                                                              │
│  buf contains:                                               │
│  ┌────────────┬────────────┬──────────────────┐              │
│  │ IP Header  │ TCP Header │ HTTP Request     │              │
│  │ 20 bytes   │ 20 bytes   │ "GET /cacerts"   │              │
│  │ dst:       │ port:6443  │                  │              │
│  │ 10.55.0.2  │            │                  │              │
│  └────────────┴────────────┴──────────────────┘              │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  Wrap in protocol - cmd/client/main.go:134                   │
│                                                              │
│  pkt := protocol.NewDataPacket(buf[:n])                      │
│                                                              │
│  ┌──────┬───────────────────────────────────────┐            │
│  │ 0x03 │ [IP Header][TCP Header][HTTP data]    │            │
│  └──────┴───────────────────────────────────────┘            │
│  type    entire original packet as payload                   │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  Encrypt - cmd/client/main.go:135                            │
│                                                              │
│  encrypted, err := c.cipher.Encrypt(pkt.Encode())            │
│                                                              │
│  ┌───────┬────────────────────────────────────┬─────────┐    │
│  │ Nonce │ Encrypted(0x03 + IP pkt)           │ AuthTag │    │
│  │ 12B   │                                    │ 16B     │    │
│  └───────┴────────────────────────────────────┴─────────┘    │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  Send over UDP - cmd/client/main.go:141                      │
│                                                              │
│  c.conn.Write(encrypted)  // c.conn is *net.UDPConn          │
│                                                              │
│  Kernel adds UDP + IP headers:                               │
│  ┌────────────┬────────────┬────────────────────────────┐    │
│  │ IP Header  │ UDP Header │ Encrypted blob             │    │
│  │ dst: relay │ port:51820 │ (contains your TCP pkt)    │    │
│  └────────────┴────────────┴────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
                            ↓
                      ══ INTERNET ══
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  RELAY receives - pkg/relay/server.go:63                     │
│                                                              │
│  n, remoteAddr, err := conn.ReadFromUDP(buf)                 │
│  // remoteAddr = Node A's public IP (203.0.113.10:43210)     │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  RELAY decrypts - pkg/relay/server.go:77                     │
│                                                              │
│  decrypted, err := s.cipher.Decrypt(encrypted)               │
│  pkt, err := protocol.Decode(decrypted)                      │
│                                                              │
│  pkt.Payload contains the original IP packet:                │
│  ┌────────────┬────────────┬──────────────────┐              │
│  │ IP Header  │ TCP Header │ HTTP data        │              │
│  │ dst:       │ port:6443  │                  │              │
│  │ 10.55.0.2  │            │                  │              │
│  └────────────┴────────────┴──────────────────┘              │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  RELAY looks up destination - pkg/relay/server.go:130        │
│                                                              │
│  // Extract dest IP from IP header                           │
│  destIP, err := protocol.GetDestIP(ipPacket)                 │
│  // destIP = 10.55.0.2                                       │
│                                                              │
│  // Lookup in registry: "who has 10.55.0.2?"                 │
│  destNode, ok := s.nodes[destIP.String()]                    │
│                                                              │
│  Node Registry:                                              │
│  ┌─────────────┬─────────────────────┐                       │
│  │ Virtual IP  │ Real UDP Endpoint   │                       │
│  ├─────────────┼─────────────────────┤                       │
│  │ 10.55.0.1   │ 203.0.113.10:43210  │ ← Node A              │
│  │ 10.55.0.2   │ 198.51.100.5:54321  │ ← Node B (target!)    │
│  └─────────────┴─────────────────────┘                       │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  RELAY re-encrypts and forwards - pkg/relay/server.go:145    │
│                                                              │
│  pkt := protocol.NewDataPacket(ipPacket)                     │
│  encrypted, err := s.cipher.Encrypt(pkt.Encode())            │
│                                                              │
│  // Send to Node B's real address                            │
│  s.conn.WriteToUDP(encrypted, destNode.Endpoint)             │
│  // destNode.Endpoint = 198.51.100.5:54321                   │
└──────────────────────────────────────────────────────────────┘
                            ↓
                      ══ INTERNET ══
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  Node B: readFromRelay() - cmd/client/main.go:145            │
│                                                              │
│  n, err := c.conn.Read(buf)     // receive UDP               │
│  decrypted := c.cipher.Decrypt() // decrypt                  │
│  pkt := protocol.Decode()        // unwrap                   │
│                                                              │
│  pkt.Payload contains:                                       │
│  ┌────────────┬────────────┬──────────────────┐              │
│  │ IP Header  │ TCP Header │ HTTP Request     │              │
│  │ dst:       │ port:6443  │ "GET /cacerts"   │              │
│  │ 10.55.0.2  │            │                  │              │
│  └────────────┴────────────┴──────────────────┘              │
└──────────────────────────────────────────────────────────────┘
                            ↓
┌──────────────────────────────────────────────────────────────┐
│  Write to TUN - cmd/client/main.go:177                       │
│                                                              │
│  c.dev.Write(pkt.Payload)  // write original TCP packet      │
│                                                              │
│  Kernel receives the TCP packet on tun0                      │
│  Sees dst=10.55.0.2 (that's me!)                             │
│  Delivers to k3s listening on :6443                          │
└──────────────────────────────────────────────────────────────┘
```

## Key Insight

The relay **decrypts to read the destination IP**, then **re-encrypts before forwarding**. It only reads IP headers to route packets - it never sees your actual HTTP/application data unencrypted.

## Key Code Paths

| Function | File | Purpose |
|----------|------|---------|
| `readFromTUN()` | cmd/client/main.go:119 | Read IP packets from TUN |
| `readFromRelay()` | cmd/client/main.go:145 | Receive from relay, write to TUN |
| `handlePacket()` | pkg/relay/server.go:76 | Relay receives and routes |
| `handleData()` | pkg/relay/server.go:130 | Lookup dest, forward packet |

## Why UDP for the Outer Layer?

| | TCP | UDP |
|---|---|---|
| Connection | Stateful | Stateless |
| Overhead | High (handshake, ACK) | Low |
| NAT traversal | Harder | Easier |
| Retransmission | Built-in | None |

Your **inner** TCP handles retransmission and ordering. Adding outer TCP would cause "TCP over TCP" problems (double retransmission, head-of-line blocking).

UDP is perfect: lightweight transport, let inner protocol handle reliability.
