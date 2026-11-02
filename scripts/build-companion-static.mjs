import { mkdir, readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { runInContext } from "node:vm";

const require = createRequire(
  new URL("../packages/frontend/package.json", import.meta.url),
);
const { JSDOM } = require("jsdom");

/** 从实际渲染源码导出完整静态形象，历史头像只加载图片，不创建运行时引擎。 */
export async function buildCompanionStatic(root, styles, order, check) {
  const dom = new JSDOM("<!doctype html><body></body>", {
    runScripts: "outside-only",
  });
  try {
    const context = dom.getInternalVMContext();
    // 固定时钟和采样种子，保证生成检查可复现。
    runInContext(
      "Math.random = () => 0.5; performance.now = () => 0;",
      context,
    );
    const materials = {};
    for (const [style, material] of Object.entries(styles)) {
      const body = await readFile(
        new URL(`assets/bodies/${style}-256.png`, root),
      );
      materials[style] = {
        ...material,
        images: { 256: `data:image/png;base64,${body.toString("base64")}` },
      };
    }
    dom.window.AllyBodies = materials;
    for (const file of order) {
      runInContext(
        await readFile(new URL(`source/${file}`, root), "utf8"),
        context,
      );
    }
    const directory = new URL("assets/static/", root);
    if (!check) await mkdir(directory, { recursive: true });
    for (const style of Object.keys(styles)) {
      const host = dom.window.document.createElement("div");
      const engine = dom.window.NeuxBot.create(host, {
        style,
        emotion: "02",
        resolution: 256,
        autostart: false,
        idle: false,
      });
      engine.ball.setReduced(true);
      engine.renderStatic();
      const svg = host.querySelector("svg");
      if (!svg) throw new Error(`静态头像未生成：${style}`);
      const content =
        new dom.window.XMLSerializer().serializeToString(svg) + "\n";
      const target = new URL(`${style}.svg`, directory);
      if (check) {
        if ((await readFile(target, "utf8")) !== content) {
          throw new Error(
            `静态头像与源码不一致：${style}，请运行 pnpm companion:generate`,
          );
        }
      } else {
        await writeFile(target, content);
      }
      engine.destroy();
    }
  } finally {
    dom.window.close();
  }
}
