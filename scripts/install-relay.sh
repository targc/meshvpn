#!/bin/bash
set -e

VPN_KEY="${VPN_KEY:?VPN_KEY is required}"
LISTEN="${LISTEN:-:51820}"

echo "==> Downloading relay server..."
curl -fsSL -o /usr/local/bin/s2s-relay "${RELAY_URL:-https://example.com/relay-linux}"
chmod +x /usr/local/bin/s2s-relay

echo "==> Creating relay service..."
cat > /etc/systemd/system/s2s-relay.service <<EOF
[Unit]
Description=S2S VPN Relay
After=network.target

[Service]
ExecStart=/usr/local/bin/s2s-relay --listen "$LISTEN" --key "$VPN_KEY"
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now s2s-relay

echo "==> Done! Relay listening on $LISTEN"
