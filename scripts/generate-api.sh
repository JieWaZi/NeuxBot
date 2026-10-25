#!/bin/sh

set -eu

ALLY_API_TOOL_DIRECTORY=$(mktemp -d)
ALLY_PUBLIC_SOURCE=""
cleanup() {
  rm -rf "$ALLY_API_TOOL_DIRECTORY"
  if [ -n "$ALLY_PUBLIC_SOURCE" ]; then
    rm -f "$ALLY_PUBLIC_SOURCE"
  fi
}
trap cleanup EXIT HUP INT TERM

(
  cd server
  # 生成器跟随仓库锁定的 ginx 版本，避免机器上的旧 toolx 漏识别路由。
  ALLY_API_GINX_VERSION=$(go list -m -f '{{.Version}}' github.com/shrewx/ginx)
  ALLY_API_GO_VERSION=$(go env GOVERSION)
  GOBIN="$ALLY_API_TOOL_DIRECTORY" GOTOOLCHAIN="$ALLY_API_GO_VERSION" \
    go install "github.com/shrewx/ginx/pkg/toolx@$ALLY_API_GINX_VERSION"
  PATH="$ALLY_API_TOOL_DIRECTORY:$PATH"
  export PATH
  go generate ./constants/...
  go generate ./constants/status_error/...
  gofmt -w constants/*__generated.go constants/status_error/*__generated.go
  go generate ./cmd/ally-core
  mv cmd/ally-core/openapi.json openapi.json
)

node scripts/normalize-openapi.mjs server/openapi.json

ALLY_PUBLIC_SOURCE=$(mktemp)
(cd server && go run ./cmd/integration-contract "$ALLY_PUBLIC_SOURCE")
node scripts/public-openapi.mjs "$ALLY_PUBLIC_SOURCE" server/integration-openapi.json
node scripts/normalize-openapi.mjs server/integration-openapi.json

pnpm --dir packages/frontend api:generate
