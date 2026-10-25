import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  readFile,
  writeFile,
  copyFile,
  rm,
  access,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

test("API generation uses the locked tool version and removes temporary tools", async () => {
  const root = await mkdtemp(
    path.join(os.tmpdir(), "neux-api-generator-test-"),
  );
  try {
    for (const directory of [
      "bin",
      "scripts",
      "server/constants/status_error",
      "server/cmd/ally-core",
    ])
      await mkdir(path.join(root, directory), { recursive: true });
    await copyFile(
      new URL("./generate-api.sh", import.meta.url),
      path.join(root, "scripts/generate-api.sh"),
    );
    await writeFile(
      path.join(root, "server/constants/fixture__generated.go"),
      "fixture",
    );
    await writeFile(
      path.join(root, "server/constants/status_error/fixture__generated.go"),
      "fixture",
    );
    const log = path.join(root, "calls.log");
    await writeFile(
      path.join(root, "bin/go"),
      `#!/bin/sh
set -eu
printf '%s|%s|%s\\n' "$*" "${"${GOBIN:-}"}" "${"${GOTOOLCHAIN:-}"}" >> "$ALLY_GENERATOR_TEST_LOG"
case "$1" in
  list) printf 'v1.2.7-locked\\n' ;;
  env) printf 'go1.25.8\\n' ;;
  install)
    if [ "${"${ALLY_GENERATOR_TEST_FAIL:-}"}" = 1 ]; then exit 7; fi
    printf '#!/bin/sh\\nexit 0\\n' > "$GOBIN/toolx"
    chmod +x "$GOBIN/toolx"
    ;;
  generate)
    command -v toolx >> "$ALLY_GENERATOR_TEST_LOG"
    if [ "$2" = ./cmd/ally-core ]; then printf '{}' > cmd/ally-core/openapi.json; fi
    ;;
esac
`,
      { mode: 0o700 },
    );
    for (const command of ["gofmt", "node", "pnpm"])
      await writeFile(path.join(root, "bin", command), "#!/bin/sh\nexit 0\n", {
        mode: 0o700,
      });
    const env = {
      ...process.env,
      PATH: `${path.join(root, "bin")}${path.delimiter}${process.env.PATH}`,
      ALLY_GENERATOR_TEST_LOG: log,
    };
    const run = spawnSync("sh", ["scripts/generate-api.sh"], {
      cwd: root,
      env,
      encoding: "utf8",
    });
    assert.equal(run.status, 0, run.stderr);
    const calls = (await readFile(log, "utf8")).trim().split("\n");
    const install = calls.find((line) => line.startsWith("install "));
    assert.ok(install, "the generator must build its own locked tool");
    const [command, directory, toolchain] = install.split("|");
    assert.equal(
      command,
      "install github.com/shrewx/ginx/pkg/toolx@v1.2.7-locked",
    );
    assert.equal(toolchain, "go1.25.8");
    assert.equal(
      calls.filter((line) => line === path.join(directory, "toolx")).length,
      3,
    );
    await assert.rejects(access(directory));
    await writeFile(
      path.join(root, "server/openapi.json"),
      "keep existing generated API",
    );
    const failed = spawnSync("sh", ["scripts/generate-api.sh"], {
      cwd: root,
      env: { ...env, ALLY_GENERATOR_TEST_FAIL: "1" },
      encoding: "utf8",
    });
    assert.equal(failed.status, 7);
    assert.equal(
      await readFile(path.join(root, "server/openapi.json"), "utf8"),
      "keep existing generated API",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
