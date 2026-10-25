import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  writeFile,
  readFile,
  rm,
  access,
  symlink,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { packageNativeRenderer } from "./package-native-renderer.mjs";
import { loadModule } from "../desktop/electron/tests/load-module.mjs";
const { rendererFile, rendererResponse } = loadModule(
  "main/renderer-protocol.ts",
);
async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), "ally-native-renderer-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const source = path.join(root, "dist"),
    output = path.join(root, "packed");
  await mkdir(path.join(source, "assets"), { recursive: true });
  return { root, source, output };
}
const plainFetch = async (url) =>
  new Response(await readFile(fileURLToPath(url)));
test("原生压缩交付恢复完全相同字节、MIME和HTML脚本CSP", async (t) => {
  const { source, output } = await fixture(t);
  const script = 'export const value = "' + "中文 renderer ".repeat(600) + '";';
  const html =
    '<script>window.ready=true</script><script src="/assets/app.js"></script>';
  await writeFile(path.join(source, "assets/app.js"), script);
  await writeFile(path.join(source, "index.html"), html);
  await writeFile(path.join(source, "assets/app.js.map"), "map");
  const size = await packageNativeRenderer(source, output);
  assert.equal(size.compressedFiles, 1);
  assert.ok(size.packagedBytes < size.originalBytes);
  await assert.rejects(access(path.join(output, "assets/app.js")));
  await assert.rejects(access(path.join(output, "assets/app.js.map")));
  const response = await rendererResponse(
    output,
    "ally://app/assets/app.js",
    plainFetch,
  );
  assert.equal(await response.text(), script);
  assert.match(response.headers.get("content-type"), /javascript/);
  assert.equal(response.headers.get("content-encoding"), null);
  const page = await rendererResponse(
    output,
    "ally://app/index.html",
    plainFetch,
  );
  assert.equal(await page.text(), html);
  assert.match(page.headers.get("content-security-policy"), /sha256-/);
  assert.match(
    page.headers.get("content-security-policy"),
    /connect-src[^;]*ws:\/\/127\.0\.0\.1:\*/,
  );
  await assert.rejects(rendererFile(output, "ally://app/missing.js"));
});
test("压缩回退保留协议、目录和真实路径边界", async (t) => {
  const { root, source, output } = await fixture(t);
  await packageNativeRenderer(source, output);
  const secret = path.join(root, "outside.js.br");
  await writeFile(secret, "secret");
  await symlink(secret, path.join(output, "escape.js.br"));
  for (const url of [
    "https://app/escape.js",
    "ally://other/escape.js",
    "ally://app/%2F..%2Foutside.js",
    "ally://app/escape.js",
  ]) {
    await assert.rejects(rendererFile(output, url));
  }
  await assert.rejects(packageNativeRenderer(source, root), /dist 外/);
  await assert.rejects(
    packageNativeRenderer(source, path.join(source, "nested")),
    /dist 外/,
  );
});
