#!/bin/sh

set -eu

ALLY_GENERATED_CHECK_DIR=$(mktemp -d)
cleanup() {
  rm -r "$ALLY_GENERATED_CHECK_DIR"
}
trap cleanup EXIT INT TERM

cp server/openapi.json "$ALLY_GENERATED_CHECK_DIR/openapi.json"
cp server/integration-openapi.json "$ALLY_GENERATED_CHECK_DIR/integration-openapi.json"
if [ -d packages/frontend/src/shared/api/generated ]; then
  cp -R packages/frontend/src/shared/api/generated "$ALLY_GENERATED_CHECK_DIR/generated"
fi

sh ./scripts/generate-api.sh

diff -u "$ALLY_GENERATED_CHECK_DIR/openapi.json" server/openapi.json
diff -u "$ALLY_GENERATED_CHECK_DIR/integration-openapi.json" server/integration-openapi.json
if [ -d "$ALLY_GENERATED_CHECK_DIR/generated" ]; then
  diff -ru "$ALLY_GENERATED_CHECK_DIR/generated" packages/frontend/src/shared/api/generated
fi
