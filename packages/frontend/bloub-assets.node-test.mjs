import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import {
  mkdir,
  mkdtemp,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { URL } from "node:url";
import { build, createServer } from "vite";
import { bloubTexturePreload } from "./bloub-assets.ts";

async function fixture(t) {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "neuxbot-bloub-assets-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  const frontend = path.join(root, "packages/frontend");
  const renderer = path.join(root, "desktop");
  const dependency = path.join(root, "node_modules/@neuxbot/bloub");
  const assets = path.join(dependency, "neuxbot/materials");
  await mkdir(frontend, { recursive: true });
  await mkdir(renderer, { recursive: true });
  await mkdir(assets, { recursive: true });
  await mkdir(path.join(dependency, "neuxbot/src/materials"), {
    recursive: true,
  });
  await writeFile(path.join(frontend, "package.json"), '{"private":true}');
  await writeFile(
    path.join(dependency, "package.json"),
    JSON.stringify({
      name: "@neuxbot/bloub",
      type: "module",
      exports: {
        "./materials": "./neuxbot/src/materials/catalogue.ts",
        "./assets/*": "./neuxbot/materials/*",
      },
    }),
  );
  await writeFile(path.join(assets, "body-plush.png"), Buffer.alloc(8192, 1));
  await writeFile(
    path.join(assets, "body-particles.svg"),
    `<svg xmlns="http://www.w3.org/2000/svg">${" ".repeat(8192)}</svg>`,
  );
  await writeFile(
    path.join(dependency, "neuxbot/src/materials/catalogue.ts"),
    `export const TEXTURES = {
      plush: new URL("../../materials/body-plush.png", import.meta.url).href,
      particles: new URL("../../materials/body-particles.svg", import.meta.url).href,
    };`,
  );
  const desktopHTML = await readFile(
    new URL("../../desktop/index.html", import.meta.url),
    "utf8",
  );
  const preload = desktopHTML.match(/<link\s+rel="preload"[\s\S]*?\/>/)?.[0];
  assert.ok(preload);
  const html = `<!doctype html><html><head>${preload}</head><body><script type="module" src="/main.ts"></script></body></html>`;
  await writeFile(path.join(renderer, "index.html"), html);
  await writeFile(
    path.join(renderer, "main.ts"),
    `import { TEXTURES } from "@neuxbot/bloub/materials";
     globalThis.textures = TEXTURES;
     import("@neuxbot/bloub/materials").then(x => globalThis.again = x.TEXTURES);`,
  );
  return { root, frontend, renderer, html };
}

test("Bloub 首帧 preload 与包内纹理 URL 共享构建实体，不把图片内联到 JS", async (t) => {
  const { frontend, renderer } = await fixture(t);
  for (const base of ["./", "/"]) {
    const result = await build({
      root: renderer,
      configFile: false,
      logLevel: "silent",
      base,
      plugins: [bloubTexturePreload(frontend)],
      build: { write: false },
    });
    const assets = result.output.filter(
      (item) =>
        item.type === "asset" && /body-(plush|particles)/.test(item.fileName),
    );
    assert.equal(assets.length, 2);
    const plush = assets.find((item) => item.fileName.endsWith(".png"));
    const particles = assets.find((item) => item.fileName.endsWith(".svg"));
    assert.ok(plush && particles);
    const html = String(
      result.output.find((item) => item.fileName === "index.html").source,
    );
    assert.ok(html.includes(`href="${base}${plush.fileName}"`));
    const javascript = result.output
      .filter((item) => item.type === "chunk")
      .map((item) => item.code)
      .join("\n");
    assert.ok(javascript.includes(path.basename(plush.fileName)));
    assert.ok(javascript.includes(path.basename(particles.fileName)));
    assert.ok(!javascript.includes("data:image"));
  }
});

test("Bloub 开发首帧 preload 与默认预构建依赖使用同一纹理 URL", async (t) => {
  const { root, frontend, renderer, html } = await fixture(t);
  const server = await createServer({
    root: renderer,
    configFile: false,
    logLevel: "silent",
    plugins: [bloubTexturePreload(frontend)],
    server: { middlewareMode: true, fs: { allow: [root] } },
  });
  try {
    const page = await server.transformIndexHtml("/", html);
    const main = await server.transformRequest("/main.ts");
    await server.waitForRequestsIdle();
    const imported = main.code.match(/from "([^"]+)"/)?.[1];
    assert.ok(imported);
    const catalogue = await server.transformRequest(imported);
    const textureURL = catalogue.code.match(/plush: new URL\("([^"]+)"/)?.[1];
    assert.ok(textureURL?.startsWith("/@fs/"));
    assert.ok(page.includes(`href="${textureURL}"`));
  } finally {
    await server.close();
  }
});
