import { execFileSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const publicDir = path.join(root, "packages/frontend/public");
const resourceDir = path.join(root, "desktop/resources");
const temporaryDir = mkdtempSync(path.join(tmpdir(), "neuxbot-icons-"));
const logo = readFileSync(path.join(publicDir, "ally-logo.svg"), "utf8");
const darkLogo = logo.replaceAll("#1c1c1c", "#f7f7f7");
const templateLogo = logo.replaceAll(/#[0-9a-f]{6}/g, "#000000");

function rasterize(source, target, size) {
  execFileSync(
    "sips",
    [
      "-s",
      "format",
      "png",
      "-z",
      `${size}`,
      `${size}`,
      source,
      "--out",
      target,
    ],
    { stdio: "pipe" },
  );
}

function applicationIcon(inkLogo, background) {
  // 底板和标志共享矢量母版；外侧透明留白用于 macOS Dock 的图标对齐。
  const mark = inkLogo.replace(/<\/?svg[^>]*>/g, "");
  return `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">
  <rect x="100" y="100" width="824" height="824" rx="185" fill="${background}"/>
  <g transform="translate(220 220) scale(1.825)">${mark}</g>
</svg>`;
}

function writeWindowsIcon(source, target, sizes) {
  // ICO 目录包含各分辨率的 PNG，避免 Windows 放大单张小图时模糊。
  const header = Buffer.alloc(6 + sizes.length * 16);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(sizes.length, 4);
  let offset = header.length;
  const images = sizes.map((size, index) => {
    const raster = path.join(temporaryDir, `windows-${size}.png`);
    rasterize(source, raster, size);
    const data = readFileSync(raster);
    const entry = 6 + index * 16;
    header[entry] = size === 256 ? 0 : size;
    header[entry + 1] = size === 256 ? 0 : size;
    header.writeUInt16LE(1, entry + 4);
    header.writeUInt16LE(32, entry + 6);
    header.writeUInt32LE(data.length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    offset += data.length;
    return data;
  });
  writeFileSync(target, Buffer.concat([header, ...images]));
}

try {
  const lightSource = path.join(publicDir, "ally-logo.svg");
  const darkSource = path.join(publicDir, "ally-logo-dark.svg");
  const traySource = path.join(resourceDir, "tray/allyTemplate.svg");
  writeFileSync(darkSource, darkLogo);
  writeFileSync(traySource, templateLogo);
  rasterize(lightSource, path.join(publicDir, "ally-logo.png"), 1024);

  for (const [name, source, background] of [
    ["app-icon-source", logo, "#fafafa"],
    ["app-icon-dark", darkLogo, "#161616"],
  ]) {
    const tile = path.join(temporaryDir, `${name}.svg`);
    writeFileSync(tile, applicationIcon(source, background));
    rasterize(tile, path.join(resourceDir, `${name}.png`), 1024);
  }

  const iconset = path.join(temporaryDir, "NeuxBot.iconset");
  mkdirSync(iconset);
  const appSource = path.join(resourceDir, "app-icon-source.png");
  for (const size of [16, 32, 128, 256, 512]) {
    rasterize(appSource, path.join(iconset, `icon_${size}x${size}.png`), size);
    rasterize(
      appSource,
      path.join(iconset, `icon_${size}x${size}@2x.png`),
      size * 2,
    );
  }
  execFileSync("iconutil", [
    "-c",
    "icns",
    iconset,
    "-o",
    path.join(resourceDir, "icon.icns"),
  ]);
  writeWindowsIcon(
    appSource,
    path.join(resourceDir, "icon.ico"),
    [16, 24, 32, 48, 64, 128, 256],
  );

  // macOS 模板图由系统着色；Windows 托盘保留彩色标志与浅色底板。
  rasterize(traySource, path.join(resourceDir, "tray/allyTemplate.png"), 16);
  rasterize(traySource, path.join(resourceDir, "tray/allyTemplate@2x.png"), 32);
  writeWindowsIcon(
    appSource,
    path.join(resourceDir, "tray/ally.ico"),
    [16, 20, 24, 32, 48, 64],
  );
  process.stdout.write("已从 ally-logo.svg 生成浅暗品牌、应用与托盘图标。\n");
} finally {
  rmSync(temporaryDir, { recursive: true, force: true });
}
