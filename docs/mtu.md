# MTU (Maximum Transmission Unit)

## What is MTU?

MTU is the maximum packet size (in bytes) that can be sent over a network interface.

- Standard Ethernet MTU: **1500 bytes**
- MeshVPN default MTU: **1400 bytes**

## Why VPN needs lower MTU

VPN wraps original packets with encryption and headers:

```
Original packet (up to 1500 bytes)
    ↓
┌─────────────────────────────────┐
│ New IP header         (20 bytes)│
│ UDP header            (8 bytes) │
│ Nonce                 (12 bytes)│
│ ┌─────────────────────────────┐ │
│ │ Original IP packet          │ │
│ │ (encrypted)                 │ │
│ └─────────────────────────────┘ │
│ AES-GCM auth tag      (16 bytes)│
└─────────────────────────────────┘
    ↓
Total: original + ~60 bytes overhead
```

If original packet is 1500 bytes, wrapped packet becomes ~1560 bytes - too big for the network, gets dropped.

## MTU vs Bandwidth

| | MTU | Bandwidth |
|---|---|---|
| What | Max packet size | Data transfer rate |
| Unit | Bytes | Bits per second |
| Example | 1400 bytes | 100 Mbps |

Lower MTU does **not** reduce bandwidth. It just means:
- Smaller packets
- Slightly more overhead (~3-5%)

## Symptoms of MTU problems

- Ping works, but TCP connections hang
- Small requests work, large transfers fail
- SSH connects but hangs on commands with long output

## Configuration

MTU is set automatically by the client. To manually adjust:

```bash
ip link set tun0 mtu 1400
```

## Common VPN MTU values

| VPN | MTU |
|-----|-----|
| WireGuard | 1420 |
| OpenVPN | 1400 |
| IPsec | 1400 |
| MeshVPN | 1400 |
