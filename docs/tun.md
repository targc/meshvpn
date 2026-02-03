# What is TUN?

## Simple Explanation

TUN is a **virtual network interface** that lets your program send and receive IP packets directly.

```
Physical interface (eth0):  Hardware → Kernel → App
Virtual interface (tun0):   App → Kernel → App
```

## How Normal Networking Works

```
┌─────────────────────────────────────────────────────────────┐
│  curl https://google.com                                    │
│       ↓                                                     │
│  Kernel: "google.com = 142.250.x.x, route via eth0"         │
│       ↓                                                     │
│  eth0 (physical NIC)                                        │
│       ↓                                                     │
│  Wire/WiFi → Router → Internet → Google                     │
└─────────────────────────────────────────────────────────────┘
```

Packets go out through physical hardware. You can't intercept them.

## How TUN Works

```
┌─────────────────────────────────────────────────────────────┐
│  curl https://10.55.0.2:6443                                │
│       ↓                                                     │
│  Kernel: "10.55.0.0/24 route via tun0"                      │
│       ↓                                                     │
│  tun0 (virtual interface)                                   │
│       ↓                                                     │
│  VPN Client reads packet from /dev/net/tun                  │
│       ↓                                                     │
│  VPN Client encrypts and sends via UDP                      │
└─────────────────────────────────────────────────────────────┘
```

TUN lets your program **intercept** IP packets before they leave the machine.

## TUN vs TAP

| | TUN | TAP |
|---|---|---|
| Layer | Layer 3 (IP) | Layer 2 (Ethernet) |
| Packets | IP packets | Ethernet frames |
| Use case | VPN, routing | Bridging, VM networking |
| Header | IP header (20 bytes) | Ethernet header (14 bytes) + IP |

MeshVPN uses TUN because we only need IP routing, not Ethernet bridging.

## Creating TUN in Go

### Linux

```go
// pkg/tun/tun_linux.go

// 1. Open the TUN device
fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR, 0)

// 2. Configure it with ioctl
var req ifReq
copy(req.Name[:], "tun0")
req.Flags = IFF_TUN | IFF_NO_PI  // TUN mode, no packet info header

syscall.Syscall(syscall.SYS_IOCTL, fd, TUNSETIFF, &req)

// 3. Assign IP address
exec.Command("ip", "addr", "add", "10.55.0.1/24", "dev", "tun0").Run()

// 4. Bring interface up
exec.Command("ip", "link", "set", "tun0", "up").Run()

// 5. Set MTU
exec.Command("ip", "link", "set", "tun0", "mtu", "1400").Run()
```

### macOS

```go
// pkg/tun/tun_darwin.go

// 1. Create utun socket
fd, err := syscall.Socket(syscall.AF_SYSTEM, syscall.SOCK_DGRAM, 2)

// 2. Connect to utun control
syscall.Syscall(syscall.SYS_CONNECT, fd, &addr, size)
// Kernel assigns name like "utun0"

// 3. Configure with ifconfig
exec.Command("ifconfig", "utun0", "inet", "10.55.0.1", "10.55.0.1", "up").Run()

// 4. Add route
exec.Command("route", "add", "-net", "10.55.0.0/24", "-interface", "utun0").Run()
```

## Reading and Writing

Once TUN is created, it's just a file descriptor:

```go
// Read IP packet from TUN (app sent data to 10.55.x.x)
n, err := tunFile.Read(buf)
// buf now contains: [IP Header][TCP/UDP Header][Payload]

// Write IP packet to TUN (deliver to local app)
tunFile.Write(ipPacket)
// Kernel delivers to app listening on that IP/port
```

## Packet Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        USER SPACE                           │
│                                                             │
│  ┌─────────────┐                      ┌─────────────┐       │
│  │    App      │                      │ VPN Client  │       │
│  │  (curl)     │                      │             │       │
│  └──────┬──────┘                      └──────┬──────┘       │
│         │ send(10.55.0.2)                    │              │
│         │                                    │ read()/write()
├─────────┼────────────────────────────────────┼──────────────┤
│         │              KERNEL                │              │
│         ▼                                    ▼              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                  Routing Table                      │    │
│  │  10.55.0.0/24 → tun0                                │    │
│  │  0.0.0.0/0    → eth0 (default)                      │    │
│  └─────────────────────────────────────────────────────┘    │
│         │                                    ▲              │
│         ▼                                    │              │
│  ┌─────────────┐                      ┌─────────────┐       │
│  │    tun0     │ ◄────────────────────│    tun0     │       │
│  │   (send)    │     IP packets       │  (receive)  │       │
│  └─────────────┘                      └─────────────┘       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## What Happens Step by Step

### Outbound (App → VPN → Network)

```
1. App calls: connect(10.55.0.2:6443)

2. Kernel looks up routing table:
   "10.55.0.2 matches 10.55.0.0/24 → tun0"

3. Kernel builds IP packet:
   ┌────────────┬────────────┬──────────┐
   │ IP: dst    │ TCP: port  │ Data     │
   │ 10.55.0.2  │ 6443       │ ...      │
   └────────────┴────────────┴──────────┘

4. Kernel writes packet to tun0

5. VPN client reads from /dev/net/tun:
   n, _ := tunFile.Read(buf)
   // buf = the IP packet

6. VPN client encrypts and sends via real network
```

### Inbound (Network → VPN → App)

```
1. VPN client receives encrypted packet from relay

2. VPN client decrypts, gets IP packet:
   ┌────────────┬────────────┬──────────┐
   │ IP: dst    │ TCP: port  │ Data     │
   │ 10.55.0.2  │ 6443       │ ...      │
   └────────────┴────────────┴──────────┘

3. VPN client writes to TUN:
   tunFile.Write(ipPacket)

4. Kernel receives packet on tun0

5. Kernel: "dst 10.55.0.2 is my IP on tun0, deliver locally"

6. Kernel delivers to app listening on :6443
```

## Why TUN for VPN?

| Feature | Why it matters |
|---------|----------------|
| Works at IP level | Route any protocol (TCP, UDP, ICMP) |
| Kernel handles routing | Apps don't need modification |
| Just a file descriptor | Simple read()/write() API |
| Transparent to apps | curl, k3s, etc. work unchanged |

## Verify TUN is Working

```bash
# Check interface exists
ip addr show tun0

# Check routing
ip route | grep tun0

# Test connectivity
ping 10.55.0.2
```

## Common Issues

| Issue | Cause | Fix |
|-------|-------|-----|
| `permission denied` | Need root | Run with `sudo` |
| `no such device` | TUN module not loaded | `modprobe tun` |
| `file exists` | Interface already exists | Use different name |
| Packets not routing | Missing route | Check `ip route` |
