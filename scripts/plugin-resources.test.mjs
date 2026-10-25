import { createRequire } from "node:module";
const requireDesktop = createRequire(
  new URL("../desktop/package.json", import.meta.url),
);
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtemp,
  readFile,
  rm,
  mkdir,
  writeFile,
  readdir,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {
  loadLock,
  verifyDigest,
  prepareCatalog,
  selectAsset,
} from "./plugin-resources.mjs";

test("fixed source rejects missing provenance and tampered bytes", async () => {
  assert.throws(
    () => verifyDigest(Buffer.from("tampered"), "a".repeat(64)),
    /摘要/,
  );
  await assert.rejects(
    () => loadLock({ schemaVersion: 1, source: {} }),
    /来源/,
  );
});
test("target chooses only exact fixed native asset", async () => {
  const lock = await loadLock();
  assert.equal(
    selectAsset(lock, "darwin-arm64").file,
    "lark-cli-1.0.98-darwin-arm64.tar.gz",
  );
  assert.equal(
    selectAsset(lock, "win32-x64").file,
    "lark-cli-1.0.98-windows-amd64.zip",
  );
  assert.throws(() => selectAsset(lock, "unknown"), /目标/);
});
test("desktop catalog preparation emits only an HTTP source and removes stale embedded packages", async () => {
  const output = await mkdtemp(path.join(os.tmpdir(), "plugin-catalog-"));
  try {
    await mkdir(path.join(output, "catalog"), { recursive: true });
    await writeFile(path.join(output, "catalog", "old.zip"), "old package");
    await writeFile(path.join(output, "catalog.json"), "old index");
    await prepareCatalog(output);
    const sources = JSON.parse(
      await readFile(path.join(output, "sources.json"), "utf8"),
    );
    assert.deepEqual(sources, {
      schemaVersion: 1,
      sources: [
        {
          url: "https://github.com/JieWaZi/neux-plugins/releases/latest/download/catalog.json",
        },
      ],
    });
    assert.deepEqual((await readdir(output)).sort(), ["sources.json"]);
  } finally {
    await rm(output, { recursive: true, force: true });
  }
});

test("both providers use pinned assets for all six desktop targets", async () => {
  const lock = await loadLock();
  for (const target of [
    "darwin-amd64",
    "darwin-arm64",
    "windows-amd64",
    "windows-arm64",
    "linux-amd64",
    "linux-arm64",
  ]) {
    const asset = selectAsset(lock, target, "github");
    assert.equal(asset.target, target);
    assert.equal(
      asset.file,
      "github-cli-fc4b137cdef0a6bd28fd461b7cf9c84a5812a8cd.tar.gz",
    );
    assert.equal(
      asset.url,
      "https://github.com/cli/cli/archive/fc4b137cdef0a6bd28fd461b7cf9c84a5812a8cd.tar.gz",
    );
  }
  const incomplete = structuredClone(lock);
  incomplete.providers = incomplete.providers.filter(
    (p) => p.name !== "github",
  );
  await assert.rejects(() => loadLock(incomplete), /来源/);
});
