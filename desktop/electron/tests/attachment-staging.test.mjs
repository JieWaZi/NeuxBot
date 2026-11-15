import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadModule } from "./load-module.mjs";

const { createAttachmentStaging } = loadModule("main/attachment-staging.ts");

test("剪贴板附件保存原始字节，同名文件独立暂存，释放时清理", async () => {
  const top = await mkdtemp(path.join(tmpdir(), "bot-staging-test-"));
  const staging = createAttachmentStaging(top);
  try {
    const first = staging.stage("截图.png", new Uint8Array([0, 1, 255]));
    const second = staging.stage("截图.png", new Uint8Array([2, 3]));
    const [firstPath, secondPath] = await Promise.all([first, second]);
    assert.notEqual(firstPath, secondPath);
    assert.equal(path.basename(firstPath), "截图.png");
    assert.deepEqual(await readFile(firstPath), Buffer.from([0, 1, 255]));
    assert.deepEqual(await readFile(secondPath), Buffer.from([2, 3]));
    await staging.dispose();
    await staging.dispose();
    await assert.rejects(readFile(firstPath), { code: "ENOENT" });
    await assert.rejects(staging.stage("a.txt", new Uint8Array()), /已关闭/);
  } finally {
    await staging.dispose();
    await rm(top, { recursive: true, force: true });
  }
});

test("暂存名称不能指定任意目录，释放会等待正在写入的附件", async () => {
  const staging = createAttachmentStaging();
  for (const name of ["", ".", "..", "../escape", "a/b", "a\\b", "a\0b"])
    await assert.rejects(staging.stage(name, new Uint8Array()), /名称无效/);
  const writing = staging.stage("a.txt", new Uint8Array([5]));
  await staging.dispose();
  await assert.rejects(readFile(await writing), { code: "ENOENT" });
});
