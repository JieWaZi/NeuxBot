import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { stampReleaseVersion } from "./stamp-release-version.mjs";

test("把发布版本写入根包和桌面包", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "bot-version-"));
  const desktop = path.join(root, "desktop");
  await mkdir(desktop);
  await writeFile(
    path.join(root, "package.json"),
    `${JSON.stringify({ name: "bot", version: "0.0.0" }, null, 2)}\n`,
  );
  await writeFile(
    path.join(desktop, "package.json"),
    `${JSON.stringify({ name: "@bot/desktop", version: "0.0.0", private: true })}\n`,
  );

  await stampReleaseVersion(root, "0.2.0");

  for (const relative of ["package.json", "desktop/package.json"]) {
    const manifest = JSON.parse(
      await readFile(path.join(root, relative), "utf8"),
    );
    assert.equal(manifest.version, "0.2.0");
  }
});

test("拒绝不合规版本", async () => {
  await assert.rejects(
    stampReleaseVersion("/tmp", "v0.1.0"),
    /NEUXBOT_VERSION/,
  );
});
