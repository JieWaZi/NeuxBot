#!/bin/sh

set -eu

NEUXBOT_GENERATED_CHECK_DIR=$(mktemp -d)
cleanup() {
  rm -r "$NEUXBOT_GENERATED_CHECK_DIR"
}
trap cleanup EXIT INT TERM

cp packages/frontend/src/shared/api/status-error-catalog.generated.ts "$NEUXBOT_GENERATED_CHECK_DIR/status-error-catalog.generated.ts"
cp server/openapi.json "$NEUXBOT_GENERATED_CHECK_DIR/openapi.json"
cp server/integration-openapi.json "$NEUXBOT_GENERATED_CHECK_DIR/integration-openapi.json"
if [ -d packages/frontend/src/shared/api/generated ]; then
  cp -R packages/frontend/src/shared/api/generated "$NEUXBOT_GENERATED_CHECK_DIR/generated"
fi

sh ./scripts/generate-api.sh

diff -u "$NEUXBOT_GENERATED_CHECK_DIR/status-error-catalog.generated.ts" packages/frontend/src/shared/api/status-error-catalog.generated.ts
diff -u "$NEUXBOT_GENERATED_CHECK_DIR/openapi.json" server/openapi.json
diff -u "$NEUXBOT_GENERATED_CHECK_DIR/integration-openapi.json" server/integration-openapi.json
if [ -d "$NEUXBOT_GENERATED_CHECK_DIR/generated" ]; then
  diff -ru "$NEUXBOT_GENERATED_CHECK_DIR/generated" packages/frontend/src/shared/api/generated
fi
