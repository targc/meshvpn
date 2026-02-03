#!/bin/bash
set -e

RELAY_ADDR="${RELAY_ADDR:?RELAY_ADDR is required}"
VPN_KEY="${VPN_KEY:?VPN_KEY is required}"
VPN_IP="${VPN_IP:?VPN_IP is required (e.g., 10.99.0.2/24)}"
K3S_MASTER="${K3S_MASTER:-10.99.0.1}"
K3S_TOKEN="${K3S_TOKEN:?K3S_TOKEN is required}"

echo "==> Downloading VPN client..."
rm -f /usr/local/bin/meshvpn-client
curl -fsSL -o /usr/local/bin/meshvpn-client "${CLIENT_URL:-https://example.com/client-linux}"
chmod +x /usr/local/bin/meshvpn-client

echo "==> Creating VPN service..."
cat > /etc/systemd/system/meshvpn.service <<EOF
[Unit]
Description=MeshVPN Client
After=network.target

[Service]
ExecStart=/usr/local/bin/meshvpn-client --relay "$RELAY_ADDR" --key "$VPN_KEY" --ip "$VPN_IP"
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now meshvpn

echo "==> Waiting for TUN device..."
sleep 3

NODE_IP=$(echo "$VPN_IP" | cut -d'/' -f1)

echo "==> Installing k3s agent..."
curl -sfL https://get.k3s.io | sh -s - agent \
  --server "https://${K3S_MASTER}:6443" \
  --token "$K3S_TOKEN" \
  --node-ip "$NODE_IP" \
  --flannel-iface tun0

echo "==> Done!"
