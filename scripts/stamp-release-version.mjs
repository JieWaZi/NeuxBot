import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const releaseVersionPattern = /^[0-9]+\.[0-9]+\.[0-9]+$/;

export async function stampReleaseVersion(root, version) {
  if (!releaseVersionPattern.test(version)) {
    throw new Error(`NEUXBOT_VERSION 必须是 X.Y.Z，当前为 ${version || "空"}`);
  }
  for (const relative of ["package.json", "desktop/package.json"]) {
    const file = path.join(root, relative);
    const manifest = JSON.parse(await readFile(file, "utf8"));
    manifest.version = version;
    await writeFile(file, `${JSON.stringify(manifest, null, 2)}\n`);
  }
}

const isMain =
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  await stampReleaseVersion(root, process.env.NEUXBOT_VERSION ?? "");
  console.log(`已写入发布版本 ${process.env.NEUXBOT_VERSION}`);
}
