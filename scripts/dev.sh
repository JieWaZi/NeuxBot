#!/bin/sh

set -eu

ALLY_DEV_TOKEN="${ALLY_LOCAL_TOKEN:-ally-local-development-token}"
export ALLY_LOCAL_TOKEN="$ALLY_DEV_TOKEN"
export VITE_ALLY_LOCAL_TOKEN="$ALLY_DEV_TOKEN"
export VITE_ALLY_CORE_URL="${VITE_ALLY_CORE_URL:-http://127.0.0.1:8432}"
pnpm pi:bridge:generate
export PI_MCP_MODULE_PATH="$(pwd)/resources/pi-bridge/bridge.mjs"

(
  cd server
  exec go run ./cmd/ally-core -f configs/local.yaml
) &
ALLY_CORE_PID=$!

cleanup() {
  kill "$ALLY_CORE_PID" 2>/dev/null || true
  wait "$ALLY_CORE_PID" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

pnpm --dir desktop dev
