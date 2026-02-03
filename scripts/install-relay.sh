#!/bin/bash
set -e

VPN_KEY="${VPN_KEY:?VPN_KEY is required}"
LISTEN="${LISTEN:-:51820}"

echo "==> Downloading relay server..."
rm -f /usr/local/bin/meshvpn-relay
curl -fsSL -o /usr/local/bin/meshvpn-relay "${RELAY_URL:-https://example.com/relay-linux}"
chmod +x /usr/local/bin/meshvpn-relay

echo "==> Creating relay service..."
cat > /etc/systemd/system/meshvpn-relay.service <<EOF
[Unit]
Description=MeshVPN Relay
After=network.target

[Service]
ExecStart=/usr/local/bin/meshvpn-relay --listen "$LISTEN" --key "$VPN_KEY"
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now meshvpn-relay

echo "==> Done! Relay listening on $LISTEN"
