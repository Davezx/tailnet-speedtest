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

# Build guard: this is a small-memory shared server. Refuse to build when
# free memory is low, and compile single-threaded to cap peak usage.
AVAIL_MB=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
if [ "${AVAIL_MB:-0}" -lt 1200 ]; then
  echo "aborting: only ${AVAIL_MB}MB memory available (<1200MB). Stop heavy services or build elsewhere."
  exit 1
fi
export GOFLAGS="-p=1 ${GOFLAGS:-}"   # single-threaded compile: slower but small peak RSS

go build -trimpath -o tailnet-speedtest .

sudo install -m 0755 tailnet-speedtest /usr/local/bin/tailnet-speedtest
sudo install -m 0644 tailnet-speedtest.service /etc/systemd/system/tailnet-speedtest.service
if [ -n "$ADDR" ]; then
  sudo sed -i "s|-addr :8080|-addr ${ADDR}|" /etc/systemd/system/tailnet-speedtest.service
fi
sudo systemctl daemon-reload
sudo systemctl enable --now tailnet-speedtest
sudo systemctl status tailnet-speedtest --no-pager
