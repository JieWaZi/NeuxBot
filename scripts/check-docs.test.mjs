import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { copyFile, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), "neux-docs-check-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, "scripts"));
  await copyFile(
    new URL("./check-docs.mjs", import.meta.url),
    path.join(root, "scripts/check-docs.mjs"),
  );
  execFileSync("git", ["init", "-q"], { cwd: root });
  await writeFile(path.join(root, ".gitignore"), ".superpowers/\n");
  await writeFile(path.join(root, "README.md"), "# Project\n");
  execFileSync("git", ["add", ".gitignore", "README.md"], { cwd: root });
  await mkdir(path.join(root, ".superpowers"));
  return root;
}

function check(root) {
  return spawnSync(process.execPath, ["scripts/check-docs.mjs"], {
    cwd: root,
    encoding: "utf8",
  });
}

test("ignored local development records do not fail public documentation checks", async (t) => {
  const root = await fixture(t);
  await writeFile(
    path.join(root, ".superpowers/session.md"),
    "/Users/developer/private\n[local](missing.md)\n",
  );
  const result = check(root);
  assert.equal(result.status, 0, result.stderr);
});

test("new public Markdown files are checked before they are staged", async (t) => {
  const root = await fixture(t);
  await writeFile(path.join(root, "draft.md"), "[guide](missing.md)\n");
  const result = check(root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /draft\.md: 链接不可用 missing\.md/);
});

test("tracked documentation is still checked when its directory is ignored", async (t) => {
  const root = await fixture(t);
  await writeFile(
    path.join(root, ".superpowers/public.md"),
    "/Users/developer/private\n",
  );
  execFileSync("git", ["add", "-f", ".superpowers/public.md"], { cwd: root });
  const result = check(root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /\.superpowers\/public\.md: 包含个人目录/);
});

test("documentation removed in the working tree is not read from the old index", async (t) => {
  const root = await fixture(t);
  await writeFile(path.join(root, "obsolete.md"), "/Users/developer/private\n");
  execFileSync("git", ["add", "obsolete.md"], { cwd: root });
  await rm(path.join(root, "obsolete.md"));
  const result = check(root);
  assert.equal(result.status, 0, result.stderr);
});
