import {
  mkdir,
  readFile,
  readdir,
  rm,
  copyFile,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { brotliCompress, constants } from "node:zlib";
const compress = promisify(brotliCompress);
const extensions = new Set([".js", ".css", ".json", ".svg"]);

/** 只改变原生交付编码；原始 dist 保留供启动预算和 Web 工具检查。 */
export async function packageNativeRenderer(input, output) {
  const source = path.resolve(input),
    destination = path.resolve(output);
  const relative = path.relative(source, destination);
  const reverse = path.relative(destination, source);
  if (
    relative === "" ||
    (!reverse.startsWith(`..${path.sep}`) &&
      reverse !== ".." &&
      !path.isAbsolute(reverse)) ||
    (!relative.startsWith(`..${path.sep}`) &&
      relative !== ".." &&
      !path.isAbsolute(relative))
  )
    throw new Error("原生交付目录必须位于原始 dist 外");
  await rm(destination, { recursive: true, force: true });
  let originalBytes = 0,
    packagedBytes = 0,
    compressedFiles = 0;
  async function copy(directory, target) {
    await mkdir(target, { recursive: true });
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      if (entry.name === ".vite" || entry.name.endsWith(".map")) continue;
      const from = path.join(directory, entry.name),
        to = path.join(target, entry.name);
      if (entry.isSymbolicLink())
        throw new Error(`原生静态资源不允许软链接：${entry.name}`);
      if (entry.isDirectory()) {
        await copy(from, to);
        continue;
      }
      const bytes = await readFile(from);
      originalBytes += bytes.length;
      if (extensions.has(path.extname(entry.name)) && bytes.length > 1024) {
        const encoded = await compress(bytes, {
          params: { [constants.BROTLI_PARAM_QUALITY]: 9 },
        });
        if (encoded.length < bytes.length) {
          await writeFile(`${to}.br`, encoded);
          packagedBytes += encoded.length;
          compressedFiles++;
          continue;
        }
      }
      await copyFile(from, to);
      packagedBytes += bytes.length;
    }
  }
  await copy(source, destination);
  return { originalBytes, packagedBytes, compressedFiles };
}
if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const result = await packageNativeRenderer(
    path.join(root, "desktop/dist"),
    path.join(root, "desktop/dist-native"),
  );
  console.log(
    `原生 Renderer 无损交付：${result.originalBytes} -> ${result.packagedBytes} bytes（${result.compressedFiles} 个 Brotli 文件）`,
  );
}
