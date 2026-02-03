# MeshVPN Architecture

## Overview

MeshVPN is a relay-based VPN for connecting private nodes that cannot reach each other directly. All traffic flows through a central relay server.

## Network Topology

```
                    ┌─────────────────────┐
                    │    Relay Server     │
                    │   (Public IP)       │
                    │   :51820 UDP        │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
              ▼                ▼                ▼
        ┌──────────┐     ┌──────────┐     ┌──────────┐
        │  Node A  │     │  Node B  │     │  Node C  │
        │10.55.0.1 │     │10.55.0.2 │     │10.55.0.3 │
        │ (NAT)    │     │ (NAT)    │     │ (NAT)    │
        └──────────┘     └──────────┘     └──────────┘
```

Nodes can be behind NAT, firewalls, or in different networks. They only need outbound UDP access to the relay.

## Components

### Relay Server (`cmd/relay`)

Central hub that routes packets between nodes.

```
┌─────────────────────────────────────────────┐
│                Relay Server                 │
├─────────────────────────────────────────────┤
│  ┌─────────────────────────────────────┐    │
│  │           Node Registry             │    │
│  │  ┌───────────┬──────────────────┐   │    │
│  │  │ VirtualIP │    UDP Address   │   │    │
│  │  ├───────────┼──────────────────┤   │    │
│  │  │ 10.55.0.1 │ 203.0.113.1:4521 │   │    │
│  │  │ 10.55.0.2 │ 198.51.100.5:331 │   │    │
│  │  │ 10.55.0.3 │ 192.0.2.10:51234 │   │    │
│  │  └───────────┴──────────────────┘   │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │         Packet Router               │    │
│  │  1. Receive encrypted packet        │    │
│  │  2. Decrypt to get dest IP          │    │
│  │  3. Lookup dest in registry         │    │
│  │  4. Re-encrypt and forward          │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘
```

### Client (`cmd/client`)

Runs on each node, creates TUN device and tunnels traffic.

```
┌─────────────────────────────────────────────┐
│                   Node                      │
├─────────────────────────────────────────────┤
│                                             │
│  ┌─────────────┐       ┌─────────────┐     │
│  │ Application │       │   Client    │     │
│  │  (k3s, etc) │       │   Process   │     │
│  └──────┬──────┘       └──────┬──────┘     │
│         │                     │             │
│         ▼                     ▼             │
│  ┌─────────────────────────────────────┐   │
│  │         TUN Device (tun0)           │   │
│  │         IP: 10.55.0.x/24            │   │
│  │         MTU: 1400                   │   │
│  └─────────────────────────────────────┘   │
│                                             │
└─────────────────────────────────────────────┘
```

## Packet Flow

### Sending a packet (Node A → Node B)

```
Node A                          Relay                          Node B
──────                          ─────                          ──────
   │                              │                              │
   │ 1. App sends to 10.55.0.2    │                              │
   │    ↓                         │                              │
   │ 2. Kernel routes to tun0     │                              │
   │    ↓                         │                              │
   │ 3. Client reads from tun0    │                              │
   │    ↓                         │                              │
   │ 4. Encrypt with AES-GCM      │                              │
   │    ↓                         │                              │
   │ 5. Send UDP to relay         │                              │
   │ ─────────────────────────────>                              │
   │                              │ 6. Decrypt packet            │
   │                              │    ↓                         │
   │                              │ 7. Read dest IP (10.55.0.2)  │
   │                              │    ↓                         │
   │                              │ 8. Lookup Node B address     │
   │                              │    ↓                         │
   │                              │ 9. Re-encrypt packet         │
   │                              │    ↓                         │
   │                              │ 10. Forward UDP to Node B    │
   │                              │ ─────────────────────────────>
   │                              │                              │ 11. Decrypt
   │                              │                              │     ↓
   │                              │                              │ 12. Write to tun0
   │                              │                              │     ↓
   │                              │                              │ 13. Kernel delivers
   │                              │                              │     to app
```

## Protocol

### Packet Types

```
┌────────────────────────────────────────────────────────┐
│ Type 0x01: Register                                    │
├────────────────────────────────────────────────────────┤
│ ┌──────┬────────────────────────────────────────────┐  │
│ │ 0x01 │  Virtual IP (4 bytes)                      │  │
│ └──────┴────────────────────────────────────────────┘  │
│ Client → Relay: "I am 10.55.0.x"                       │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│ Type 0x02: Keepalive                                   │
├────────────────────────────────────────────────────────┤
│ ┌──────┐                                               │
│ │ 0x02 │                                               │
│ └──────┘                                               │
│ Client → Relay: "I'm still here"                       │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│ Type 0x03: Data                                        │
├────────────────────────────────────────────────────────┤
│ ┌──────┬────────────────────────────────────────────┐  │
│ │ 0x03 │  Encrypted IP Packet                       │  │
│ └──────┴────────────────────────────────────────────┘  │
│ Contains full IP packet (header + payload)             │
└────────────────────────────────────────────────────────┘
```

### Encryption

```
┌─────────────────────────────────────────────────────────┐
│                    Encryption Flow                      │
├─────────────────────────────────────────────────────────┤
│                                                         │
│   Pre-Shared Key (PSK)                                  │
│         │                                               │
│         ▼                                               │
│   ┌───────────┐                                         │
│   │  SHA-256  │                                         │
│   └─────┬─────┘                                         │
│         │                                               │
│         ▼                                               │
│   256-bit AES Key                                       │
│         │                                               │
│         ▼                                               │
│   ┌───────────────────────────────────────────────┐     │
│   │              AES-256-GCM                      │     │
│   │  ┌───────┬─────────────────────┬──────────┐  │     │
│   │  │ Nonce │   Ciphertext        │ Auth Tag │  │     │
│   │  │ 12B   │   (variable)        │   16B    │  │     │
│   │  └───────┴─────────────────────┴──────────┘  │     │
│   └───────────────────────────────────────────────┘     │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## TUN Device

The TUN device is a virtual network interface that operates at Layer 3 (IP).

```
┌─────────────────────────────────────────────────────────┐
│                    Network Stack                        │
├─────────────────────────────────────────────────────────┤
│                                                         │
│   Application (k3s, curl, etc)                          │
│         │                                               │
│         │ send(10.55.0.2, data)                         │
│         ▼                                               │
│   ┌───────────────────────────────────────────────┐     │
│   │              Kernel Routing                   │     │
│   │  10.55.0.0/24 → tun0                          │     │
│   └───────────────────────────────────────────────┘     │
│         │                                               │
│         │ IP packet                                     │
│         ▼                                               │
│   ┌───────────────────────────────────────────────┐     │
│   │         TUN Device (tun0)                     │     │
│   │         /dev/net/tun (Linux)                  │     │
│   │         /dev/utun (macOS)                     │     │
│   └───────────────────────────────────────────────┘     │
│         │                                               │
│         │ read() by client                              │
│         ▼                                               │
│   ┌───────────────────────────────────────────────┐     │
│   │         VPN Client Process                    │     │
│   │         Encrypt → UDP → Relay                 │     │
│   └───────────────────────────────────────────────┘     │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Connection Lifecycle

```
┌─────────────────────────────────────────────────────────┐
│                 Connection Lifecycle                    │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  1. STARTUP                                             │
│     Client creates TUN device with virtual IP           │
│     Client sends Register packet to relay               │
│                                                         │
│  2. REGISTERED                                          │
│     Relay maps virtual IP → client's UDP address        │
│     Client starts keepalive timer (30s)                 │
│                                                         │
│  3. ACTIVE                                              │
│     Client reads IP packets from TUN                    │
│     Client encrypts and sends to relay                  │
│     Relay forwards to destination node                  │
│                                                         │
│  4. KEEPALIVE                                           │
│     Client sends keepalive every 30s                    │
│     Relay updates last-seen timestamp                   │
│                                                         │
│  5. CLEANUP                                             │
│     Relay removes nodes not seen for 90s                │
│     Connection considered dead                          │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Why Relay-Based?

| Approach | Pros | Cons |
|----------|------|------|
| Direct P2P | Lower latency | NAT traversal complex, not always possible |
| Relay-based | Works through any NAT/firewall | All traffic through relay |

MeshVPN uses relay-based approach for simplicity and reliability. Nodes only need outbound UDP to one known address.
