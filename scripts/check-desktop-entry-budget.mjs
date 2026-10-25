import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const entryBudgetBytes = 500_000;
const startupBudgetBytes = 4_500_000;
const platformKey = "src/platform/index.ts";
const workspaceKey = "../packages/frontend/src/app/workspace.tsx";
const welcomeKeys = [
  "../vendor/companion/renderer.js",
  "../vendor/elastic-mesh/renderer.js",
  "../packages/frontend/src/app/welcome/elastic-logo.tsx",
];

/** 使用 Vite 生成的真实依赖图，避免正则漏掉压缩后的静态导入。 */
export async function readStartupAssets(desktopDist) {
  const manifest = JSON.parse(
    await readFile(path.join(desktopDist, ".vite/manifest.json"), "utf8"),
  );
  const entry = manifest["index.html"];
  if (!entry?.isEntry) throw new Error("未找到桌面端生产构建入口脚本");
  if (
    !manifest[workspaceKey] ||
    !entry.dynamicImports?.includes(workspaceKey)
  ) {
    throw new Error("未找到启动时等待的工作台入口");
  }

  function collect(key, files) {
    const chunk = manifest[key];
    if (!chunk) throw new Error(`构建清单缺少依赖：${key}`);
    if (files.has(chunk.file)) return;
    files.add(chunk.file);
    for (const imported of chunk.imports ?? []) collect(imported, files);
  }

  const entryFiles = new Set();
  collect("index.html", entryFiles);
  const startupFiles = new Set(entryFiles);
  // main.tsx 在渲染工作台前 await 此动态入口，它也属于真实启动成本。
  collect(workspaceKey, startupFiles);
  // Native 平台注入必须在 React 挂载前完成，也属于启动成本。
  if (!entry.dynamicImports?.includes(platformKey))
    throw new Error("未找到启动时等待的原生平台入口");
  collect(platformKey, startupFiles);
  // 欢迎完成前会预热的渲染代码同样属于启动成本，不能因动态导入漏记。
  for (const key of welcomeKeys) collect(key, startupFiles);
  const sizes = await Promise.all(
    Array.from(startupFiles, async (file) => ({
      file,
      size: (await stat(path.join(desktopDist, file))).size,
    })),
  );
  return {
    entryBytes: sizes.reduce(
      (total, asset) => total + (entryFiles.has(asset.file) ? asset.size : 0),
      0,
    ),
    startupBytes: sizes.reduce((total, asset) => total + asset.size, 0),
    startupFiles: sizes,
  };
}

async function checkBudget() {
  const repositoryRoot = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "..",
  );
  const assets = await readStartupAssets(
    path.join(repositoryRoot, "desktop/dist"),
  );
  console.log(
    `桌面端启动画面静态 JS：${assets.entryBytes} / ${entryBudgetBytes} bytes`,
  );
  console.log(
    `桌面端工作台启动 JS：${assets.startupBytes} / ${startupBudgetBytes} bytes（${assets.startupFiles.length} 个文件）`,
  );
  for (const asset of assets.startupFiles
    .sort((left, right) => right.size - left.size)
    .slice(0, 10)) {
    console.log(`  ${asset.size} bytes  ${asset.file}`);
  }
  if (
    assets.entryBytes > entryBudgetBytes ||
    assets.startupBytes > startupBudgetBytes
  ) {
    throw new Error("桌面端启动 JS 超出预算");
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  await checkBudget();
}
