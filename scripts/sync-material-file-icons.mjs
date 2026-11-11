import { mkdir, readFile, copyFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";

// 从下列固定提交的解压目录同步精选素材；默认图标按上游生成器的路径和默认色生成。
const revision = "ced21b9b5241c1efa502a8c46ec7363a690a7058";
const source = process.argv[2];
if (!source) throw new Error("请提供 Material Icon Theme 固定提交的解压目录");
const upstream = resolve(source);
const target = fileURLToPath(
  new URL(
    "../packages/frontend/src/shared/assets/material-icon-theme/",
    import.meta.url,
  ),
);
const names = [
  "markdown",
  "document",
  "javascript",
  "typescript",
  "react",
  "react_ts",
  "vue",
  "html",
  "css",
  "go",
  "python",
  "java",
  "ruby",
  "console",
  "json",
  "yaml",
  "toml",
  "tune",
  "git",
  "docker",
  "nodejs",
  "image",
  "pdf",
  "word",
  "table",
  "powerpoint",
  "zip",
  "rust",
  "svg",
  "log",
  "tsconfig",
];
await mkdir(target, { recursive: true });
await Promise.all(
  names.map((name) =>
    copyFile(
      join(upstream, "icons", `${name}.svg`),
      join(target, `${name}.svg`),
    ),
  ),
);
await copyFile(join(upstream, "LICENSE"), join(target, "LICENSE"));

const generatorRoot = join(upstream, "src/core/generator");
const config = await readFile(
  join(generatorRoot, "config/defaultConfig.ts"),
  "utf8",
);
const color = /const defaultColor = '(#[\da-f]+)'/i.exec(config)?.[1];
if (!color) throw new Error("上游默认图标颜色未找到");
for (const [file, constant, output] of [
  ["fileGenerator.ts", "fileIcon", "file"],
  ["folderGenerator.ts", "folderIcon", "folder"],
  ["folderGenerator.ts", "folderIconOpen", "folder-open"],
]) {
  const generator = await readFile(join(generatorRoot, file), "utf8");
  const path = new RegExp(`const ${constant} =\\s*'([^']+)';`).exec(
    generator,
  )?.[1];
  if (!path) throw new Error(`上游图标路径未找到：${constant}`);
  // 对应上游 shared/svg.ts 的 getSVG(getPath(path, color))，默认 opacity/saturation 均为 1。
  await writeFile(
    join(target, `${output}.svg`),
    `<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg"><path d="${path}" fill="${color}" /></svg>`,
  );
}
await writeFile(
  join(target, "NOTICE.md"),
  `# Material Icon Theme 文件图标

- 来源：[material-extensions/vscode-material-icon-theme](https://github.com/material-extensions/vscode-material-icon-theme/tree/${revision})
- 固定提交：\`${revision}\`
- Copyright (c) 2025 Material Extensions，采用 [MIT License](LICENSE)。
- 本目录只保存实际使用的常见类型 SVG 子集；类型图标从上游 icons 目录原样复制。
- 通用文件、文件夹与展开图标按同一提交的 fileGenerator.ts、folderGenerator.ts、shared/svg.ts 及默认颜色生成，未自定义配色。

更新命令：\`node scripts/sync-material-file-icons.mjs <固定提交的解压目录>\`。同步版本时同时更新脚本中的提交、精选清单与根第三方声明。
`,
);
console.log(
  `已同步 ${names.length + 3} 个 Material Icon Theme 图标（${revision}）`,
);
