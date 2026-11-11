import { chmod, copyFile, mkdir, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";

/** 从前端实际使用的依赖版本收集许可，不依赖包管理器的安装目录布局。 */
export async function copyBloubLicenses(root, destination) {
  const frontendRequire = createRequire(
    path.join(root, "packages/frontend/package.json"),
  );
  await mkdir(destination, { recursive: true });
  for (const name of ["LICENSE", "LICENSE.dsh-bloub-mood"]) {
    const source = frontendRequire.resolve(`@neuxbot/bloub/${name}`);
    const output = path.join(destination, name);
    await rm(output, { force: true });
    await copyFile(source, output);
    await chmod(output, 0o644);
  }
}
