import assert from "node:assert/strict";
import { mkdtemp, writeFile, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, URL } from "node:url";
import test from "node:test";
import { build } from "vite";
import { createFrontendViteConfig } from "./vite.config.ts";

async function buildAssets(root, host) {
  const result = await build({
    ...createFrontendViteConfig({ root, host, port: 1420, base: "./" }),
    configFile: false,
    logLevel: "silent",
    publicDir: false,
    build: {
      outDir: path.join(root, host),
      write: false,
      assetsInlineLimit: 0,
    },
  });
  return result.output.filter((item) => item.type === "asset");
}

test("Native 只交付 KaTeX WOFF2 与实际 PDF 解码资产，Web 保持完整回退", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "neux-native-assets-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(
    path.join(root, "index.html"),
    '<script type="module" src="/main.js"></script>',
  );
  const katexCss = fileURLToPath(
    new URL("./node_modules/katex/dist/katex.min.css", import.meta.url),
  );
  await writeFile(
    path.join(root, "main.js"),
    `import ${JSON.stringify(katexCss)};`,
  );
  const native = await buildAssets(root, "native");
  const web = await buildAssets(root, "web");
  const katexFonts = (assets, extension) =>
    assets.filter(
      (item) =>
        item.fileName.includes("KaTeX_") && item.fileName.endsWith(extension),
    );
  assert.equal(katexFonts(native, ".woff").length, 0);
  assert.equal(katexFonts(native, ".ttf").length, 0);
  assert.equal(katexFonts(web, ".woff").length, 20);
  assert.equal(katexFonts(web, ".ttf").length, 20);
  const nativeWoff2 = katexFonts(native, ".woff2");
  assert.equal(nativeWoff2.length, 20);
  assert.deepEqual(nativeWoff2, katexFonts(web, ".woff2"));
  const pdf = (assets) =>
    assets.filter((item) => item.fileName.startsWith("pdfjs-assets/"));
  const unusedSandbox = (item) =>
    /\/quickjs-eval\.(?:wasm|js)$/.test(item.fileName);
  assert.equal(pdf(web).filter(unusedSandbox).length, 2);
  assert.equal(pdf(native).filter(unusedSandbox).length, 0);
  assert.deepEqual(
    pdf(native),
    pdf(web).filter((item) => !unusedSandbox(item)),
  );
  for (const name of [
    "jbig2.wasm",
    "openjpeg.wasm",
    "qcms_bg.wasm",
    "jbig2_nowasm_fallback.js",
    "openjpeg_nowasm_fallback.js",
  ]) {
    assert.ok(
      native.some((item) => item.fileName === `pdfjs-assets/wasm/${name}`),
      name,
    );
  }
  const nativeCss = native.find((item) => item.fileName.endsWith(".css"));
  const webCss = web.find((item) => item.fileName.endsWith(".css"));
  assert.ok(nativeCss && webCss);
  assert.equal(
    String(nativeCss.source).replace(/src:[^;}]+/g, ""),
    String(webCss.source).replace(/src:[^;}]+/g, ""),
  );
});
