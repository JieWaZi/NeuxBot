#!/bin/sh

set -eu

ALLY_API_PATCH=$(pwd)/scripts/patches/ginx-openapi-recursive-errors.patch
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
  go mod download "github.com/shrewx/ginx@$ALLY_API_GINX_VERSION"
  ALLY_API_GINX_SOURCE=$(go list -m -f '{{.Dir}}' "github.com/shrewx/ginx@$ALLY_API_GINX_VERSION")
  test -n "$ALLY_API_GINX_SOURCE"
  mkdir "$ALLY_API_TOOL_DIRECTORY/ginx"
  cp -R "$ALLY_API_GINX_SOURCE/." "$ALLY_API_TOOL_DIRECTORY/ginx/"
  chmod -R u+w "$ALLY_API_TOOL_DIRECTORY/ginx"
  (
    cd "$ALLY_API_TOOL_DIRECTORY/ginx"
    # 锁定源码仅在临时副本补全递归错误图，运行依赖与模块缓存不改写。
    git apply --check "$ALLY_API_PATCH"
    git apply "$ALLY_API_PATCH"
    GOWORK=off GOFLAGS= GOTOOLCHAIN="$ALLY_API_GO_VERSION" \
      go test -mod=readonly ./pkg/openapi -run '^TestStatusErrorsRecursiveClosure$'
    GOBIN="$ALLY_API_TOOL_DIRECTORY" GOWORK=off GOFLAGS= GOTOOLCHAIN="$ALLY_API_GO_VERSION" \
      go install -mod=readonly ./pkg/toolx
  )
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
