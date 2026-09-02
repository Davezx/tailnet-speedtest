#!/usr/bin/env bash
# Build tailnet-speedtest and install it as a systemd service.
# Usage: ./install.sh [-addr :8080]
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${1:-}"

# Prefer a Go toolchain bundled in .toolchain/ (see README), else system go.
if [ -x .toolchain/go/bin/go ]; then
  export PATH="$PWD/.toolchain/go/bin:$PATH"
  export GOPATH="$PWD/.toolchain/gopath"
  export GOMODCACHE="$PWD/.toolchain/gomodcache"
fi
command -v go >/dev/null || { echo "go toolchain not found (expected .toolchain/go or system go)"; exit 1; }

go build -trimpath -o tailnet-speedtest .

sudo install -m 0755 tailnet-speedtest /usr/local/bin/tailnet-speedtest
sudo install -m 0644 tailnet-speedtest.service /etc/systemd/system/tailnet-speedtest.service
if [ -n "$ADDR" ]; then
  sudo sed -i "s|-addr :8080|-addr ${ADDR}|" /etc/systemd/system/tailnet-speedtest.service
fi
sudo systemctl daemon-reload
sudo systemctl enable --now tailnet-speedtest
sudo systemctl status tailnet-speedtest --no-pager
