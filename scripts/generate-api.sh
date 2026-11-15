#!/bin/sh

set -eu

NEUXBOT_API_PATCH=$(pwd)/scripts/patches/ginx-openapi-recursive-errors.patch
NEUXBOT_API_TOOL_DIRECTORY=$(mktemp -d)
NEUXBOT_PUBLIC_SOURCE=""
cleanup() {
  rm -rf "$NEUXBOT_API_TOOL_DIRECTORY"
  if [ -n "$NEUXBOT_PUBLIC_SOURCE" ]; then
    rm -f "$NEUXBOT_PUBLIC_SOURCE"
  fi
}
trap cleanup EXIT HUP INT TERM

(
  cd server
  # 生成器跟随仓库锁定的 ginx 版本，避免机器上的旧 toolx 漏识别路由。
  NEUXBOT_API_GINX_VERSION=$(go list -m -f '{{.Version}}' github.com/shrewx/ginx)
  NEUXBOT_API_GO_VERSION=$(go env GOVERSION)
  go mod download "github.com/shrewx/ginx@$NEUXBOT_API_GINX_VERSION"
  NEUXBOT_API_GINX_SOURCE=$(go list -m -f '{{.Dir}}' "github.com/shrewx/ginx@$NEUXBOT_API_GINX_VERSION")
  test -n "$NEUXBOT_API_GINX_SOURCE"
  mkdir "$NEUXBOT_API_TOOL_DIRECTORY/ginx"
  cp -R "$NEUXBOT_API_GINX_SOURCE/." "$NEUXBOT_API_TOOL_DIRECTORY/ginx/"
  chmod -R u+w "$NEUXBOT_API_TOOL_DIRECTORY/ginx"
  (
    cd "$NEUXBOT_API_TOOL_DIRECTORY/ginx"
    # 锁定源码仅在临时副本补全递归错误图，运行依赖与模块缓存不改写。
    git apply --check "$NEUXBOT_API_PATCH"
    git apply "$NEUXBOT_API_PATCH"
    GOWORK=off GOFLAGS= GOTOOLCHAIN="$NEUXBOT_API_GO_VERSION" \
      go test -mod=readonly ./pkg/openapi -run '^TestStatusErrorsRecursiveClosure$'
    GOBIN="$NEUXBOT_API_TOOL_DIRECTORY" GOWORK=off GOFLAGS= GOTOOLCHAIN="$NEUXBOT_API_GO_VERSION" \
      go install -mod=readonly ./pkg/toolx
  )
  PATH="$NEUXBOT_API_TOOL_DIRECTORY:$PATH"
  export PATH
  go generate ./constants/...
  go generate ./constants/status_error/...
  gofmt -w constants/*__generated.go constants/status_error/*__generated.go
  go run ./cmd/error-catalog ../packages/frontend/src/shared/api/status-error-catalog.generated.ts
  go generate ./cmd/bot-core
  mv cmd/bot-core/openapi.json openapi.json
)

pnpm --dir packages/frontend exec prettier --write src/shared/api/status-error-catalog.generated.ts

node scripts/normalize-openapi.mjs server/openapi.json

NEUXBOT_PUBLIC_SOURCE=$(mktemp)
(cd server && go run ./cmd/integration-contract "$NEUXBOT_PUBLIC_SOURCE")
node scripts/public-openapi.mjs "$NEUXBOT_PUBLIC_SOURCE" server/integration-openapi.json
node scripts/normalize-openapi.mjs server/integration-openapi.json

pnpm --dir packages/frontend api:generate
