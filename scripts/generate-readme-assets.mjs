import { build } from "esbuild";
import { createRequire } from "node:module";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const frontend = path.join(root, "packages/frontend");
const frontendRequire = createRequire(path.join(frontend, "package.json"));
// sharp 可由独立制图环境通过 NODE_PATH 提供；不启动应用或浏览器。
const sharp = frontendRequire("sharp");
const destination = path.join(root, "docs/assets/readme");
const source = path.join(destination, "source");
const temporary = await mkdtemp(path.join(tmpdir(), "neuxbot-readme-"));
const escape = (value) =>
  String(value)
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;");

const palette = {
  background: "#f8f8f9",
  ink: "#1c1c1f",
  muted: "#6a6a6a",
  accent: "#24da36",
  violet: "#8b5cf6",
};
const text = (
  x,
  y,
  value,
  size = 24,
  weight = 500,
  color = palette.ink,
  extra = "",
) =>
  `<text x="${x}" y="${y}" font-size="${size}" font-weight="${weight}" fill="${color}" ${extra}>${escape(value)}</text>`;
const rect = (x, y, width, height, fill = "#fff", radius = 20) =>
  `<rect x="${x}" y="${y}" width="${width}" height="${height}" rx="${radius}" fill="${fill}"/>`;
const image = (name, x, y, size) =>
  `<image href="${name}.png" x="${x}" y="${y}" width="${size}" height="${size}"/>`;
const svg = (title, description, height, body) =>
  `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="${height}" viewBox="0 0 1200 ${height}" role="img" aria-labelledby="title desc">
  <title id="title">${escape(title)}</title>
  <desc id="desc">${escape(description)}</desc>
  <g font-family="Arial, Helvetica, sans-serif">
    ${rect(0, 0, 1200, height, palette.background, 28)}
    ${body}
  </g>
</svg>`.replace(/[\t ]+$/gm, "");

async function rasterize(markup, filename, width = 2400) {
  // 排版源引用相邻素材；发布 PNG 在导出时内联，避免远端或绝对路径依赖。
  for (const match of [...markup.matchAll(/href="([^"#]+\.png)"/g)]) {
    const data = await readFile(path.join(source, match[1]));
    markup = markup.replaceAll(
      `href="${match[1]}"`,
      `href="data:image/png;base64,${data.toString("base64")}"`,
    );
  }
  await sharp(Buffer.from(markup)).resize({ width }).png().toFile(filename);
}

try {
  await mkdir(source, { recursive: true });
  const bundle = path.join(temporary, "native-assets.mjs");
  // 直接使用产品的角色、表情、放大眼睛及 CLI 图标组件，不复制其几何或图标路径。
  await build({
    stdin: {
      contents: `
        import { createElement } from "react";
        import { renderToStaticMarkup } from "react-dom/server";
        import { BotEngine } from "@neuxbot/bloub/engine";
        import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
        import { EXPRESSION_BY_ID } from "@neuxbot/bloub/expressions";
        import { avatarDrawing } from "@neuxbot/bloub/drawing";
        import { enlargedEyes } from "@neuxbot/bloub/eyes";
        import { REST_LOOK } from "@neuxbot/bloub/motion";
        import { renderDrawing } from "./src/shared/companion/render-drawing";
        import { RuntimeCliIcon } from "./src/shared/ui/runtime-cli-icon";
        export { tintRamp } from "@neuxbot/bloub/tint";
        export { companionAppearance, defaultCompanionAvatar, avatarPresets } from "./src/shared/companion/appearance";
        export function avatar(appearance, texture, id) {
          const engine = new BotEngine(100, "idle", SHAPE_BY_ID.get(appearance.shape).radii, EXPRESSION_BY_ID.get(appearance.expression));
          engine.setLook(REST_LOOK, 0);
          const frame = engine.sample(1.4);
          return renderToStaticMarkup(createElement("svg", {xmlns:"http://www.w3.org/2000/svg",width:720,height:720,viewBox:"-158 -158 316 316"},
            ...avatarDrawing({...frame,eyes:enlargedEyes(frame.eyes,1.3)}, "plush", appearance.color, id, texture, [], [], undefined, appearance.shape, 1.4).map(renderDrawing)));
        }
        export function cliIcon(runtimeId) {
          return renderToStaticMarkup(createElement(RuntimeCliIcon, {runtimeId, size:40}));
        }
      `,
      resolveDir: frontend,
      sourcefile: "readme-native-assets.tsx",
      loader: "tsx",
    },
    bundle: true,
    platform: "node",
    format: "esm",
    outfile: bundle,
    jsx: "automatic",
    alias: { "@": path.join(frontend, "src") },
    loader: { ".svg": "dataurl" },
    // React 的服务器渲染器包含 CommonJS 依赖，保留 Node require 语义。
    banner: {
      js: 'import {createRequire as makeRequire} from "node:module"; const require=makeRequire(import.meta.url);',
    },
    logLevel: "silent",
  });
  const native = await import(pathToFileURL(bundle).href);
  const { data: body, info } = await sharp(
    frontendRequire.resolve("@neuxbot/bloub/assets/body-plush.png"),
  )
    .ensureAlpha()
    .raw()
    .toBuffer({ resolveWithObject: true });
  const characters = [
    ["partner-lead", native.companionAppearance(native.defaultCompanionAvatar)],
    [
      "partner-research",
      { material: "plush", ...native.avatarPresets[1], color: "#14b8a6" },
    ],
    [
      "partner-build",
      { material: "plush", ...native.avatarPresets[2], color: "#f97316" },
    ],
  ];
  for (const [id, appearance] of characters) {
    const pixels = Buffer.from(body);
    const ramp = native.tintRamp(appearance.color);
    for (let index = 0; index < pixels.length; index += 4) {
      const tone = pixels[index] * 3;
      pixels[index] = ramp[tone];
      pixels[index + 1] = ramp[tone + 1];
      pixels[index + 2] = ramp[tone + 2];
    }
    const atlas = await sharp(pixels, {
      raw: { width: info.width, height: info.height, channels: 4 },
    })
      .png()
      .toBuffer();
    const markup = native.avatar(
      appearance,
      `data:image/png;base64,${atlas.toString("base64")}`,
      id,
    );
    await sharp(Buffer.from(markup))
      .png()
      .toFile(path.join(source, `${id}.png`));
  }

  const logo = (
    await readFile(path.join(frontend, "public/bot-logo.svg"), "utf8")
  ).replace(
    /<svg[^>]*>/,
    '<svg x="52" y="42" width="44" height="44" viewBox="0 0 320 320">',
  );
  const hero = svg(
    "NeuxBot — your AI partners, working together",
    "Product concept: persistent AI partners collaborate on a goal and deliver research, code changes and reports in a local-first workspace.",
    720,
    `
    <g id="brand">${logo}${text(108, 75, "NeuxBot", 30, 700)}</g>
    <g id="promise">
      ${text(56, 186, "Your AI partners.", 64, 700, palette.ink, 'letter-spacing="-2"')}
      ${text(56, 262, "Working together.", 64, 700, palette.ink, 'letter-spacing="-2"')}
      ${text(58, 319, "A local-first workspace for real work.", 26, 400, palette.muted)}
    </g>
    <g id="partners">
      <ellipse cx="886" cy="373" rx="228" ry="43" fill="#ededf0"/>
      ${image("partner-research", 617, 158, 260)}
      ${image("partner-build", 887, 151, 260)}
      ${image("partner-lead", 716, 83, 340)}
    </g>
    <g id="goal">
      ${rect(56, 380, 1088, 78, "#1c1c1f", 20)}
      <circle cx="91" cy="419" r="8" fill="${palette.accent}"/>
      ${text(116, 428, "Plan the release. Build it. Review the result.", 28, 500, "#fff")}
      <path d="M1080 407l12 12-12 12m-13-12h24" fill="none" stroke="${palette.accent}" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>
    </g>
    <g id="work-and-deliverables">
      ${rect(56, 482, 348, 182)}
      ${rect(422, 482, 348, 182)}
      ${rect(788, 482, 356, 182)}
      ${text(80, 523, "RESEARCH", 18, 700, "#087f73", 'letter-spacing="1.5"')}
      ${text(80, 562, "Knowledge → brief", 28, 600)}
      <path d="M82 593h166m-166 14h207m-207 14h135" stroke="#dedee3" stroke-width="5" stroke-linecap="round"/>
      ${text(446, 523, "BUILD", 18, 700, "#a54815", 'letter-spacing="1.5"')}
      ${text(446, 562, "Project → changes", 28, 600)}
      ${rect(446, 585, 118, 39, "#eaf7ee", 8)}
      ${text(461, 612, "+ src/app", 20, 600, "#217346")}
      ${text(812, 523, "REVIEW", 18, 700, "#7047a3", 'letter-spacing="1.5"')}
      ${text(812, 562, "Results → next step", 28, 600)}
      <path d="M815 603l7 7 14-16" fill="none" stroke="#7047a3" stroke-width="3" stroke-linecap="round"/>
      ${text(850, 611, "Ready for your review", 21, 400, palette.muted)}
    </g>
  `,
  );

  const clis = [
    ["codex", "Codex"],
    ["claude", "Claude Code"],
    ["cursor", "Cursor"],
    ["kimi", "Kimi"],
    ["pi", "Pi"],
    ["opencode", "OpenCode"],
    ["gemini", "Gemini CLI"],
    ["grok", "Grok Build"],
    ["workbuddy", "WorkBuddy"],
    ["qoder", "Qoder"],
  ];
  const cliGrid = clis
    .map(([id, name], index) => {
      const x = 56 + (index % 5) * 220;
      const y = 322 + Math.floor(index / 5) * 108;
      const icon = native
        .cliIcon(id)
        .replace(/<link[^>]*>/g, "")
        .replace(
          /<img\b[^>]*src="([^"]+)"[^>]*>/,
          '<image href="$1" width="40" height="40"/>',
        );
      return `<g id="cli-${id}">${rect(x, y, 208, 92, "#fff", 18)}<g transform="translate(${x + 18} ${y + 24})" color="${palette.ink}">${icon}</g>${text(x + 72, y + 55, name, name.length > 9 ? 21 : 23, 600)}</g>`;
    })
    .join("\n");
  const multi = svg(
    "One team. Your choice of CLI.",
    "The same partner workspace works with ten supported local CLIs: Codex, Claude Code, Cursor, Kimi, Pi, OpenCode, Gemini CLI, Grok Build, WorkBuddy and Qoder. Models and tools depend on the selected CLI and account.",
    600,
    `
    <g id="choice">
      ${text(56, 100, "One team.", 64, 700, palette.ink, 'letter-spacing="-2"')}
      ${text(56, 176, "Your choice of CLI.", 64, 700, palette.ink, 'letter-spacing="-2"')}
      ${text(58, 236, "Choose the CLI, account and model for each job.", 26, 400, palette.muted)}
    </g>
    <g id="partner-identities">
      ${image("partner-research", 782, 58, 210)}
      ${image("partner-build", 954, 53, 210)}
      ${image("partner-lead", 851, 18, 248)}
    </g>
    <g id="supported-clis">${cliGrid}</g>
    ${text(600, 557, "Partners, projects and work records stay in one workspace.", 24, 400, palette.muted, 'text-anchor="middle"')}
  `,
  );

  for (const [name, markup] of [
    ["hero", hero],
    ["multi-cli", multi],
  ]) {
    await writeFile(path.join(source, `${name}-layout.svg`), markup);
    await rasterize(markup, path.join(destination, `${name}.png`));
    console.log(`Generated docs/assets/readme/${name}.png`);
  }
} finally {
  await rm(temporary, { recursive: true, force: true });
}
