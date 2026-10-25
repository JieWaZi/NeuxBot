import { build } from "esbuild";
import { cp, mkdir, chmod, readdir } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root = path.dirname(fileURLToPath(import.meta.url));
const output = path.resolve(root, "../../resources/terminal-runtime");
const require = createRequire(import.meta.url);
await mkdir(output, { recursive: true });
await build({
  entryPoints: [path.join(root, "src/worker.ts")],
  outfile: path.join(output, "worker.cjs"),
  bundle: true,
  platform: "node",
  target: "node22",
  format: "cjs",
  external: ["node-pty"],
  legalComments: "eof",
});
const pty = path.dirname(require.resolve("node-pty/package.json"));
await cp(pty, path.join(output, "node_modules/node-pty"), {
  recursive: true,
  dereference: true,
});
// 发布目标可能不同于构建宿主；每个随包交付的 Unix helper 都须可执行。
const ptyOutput = path.join(output, "node_modules/node-pty");
const helpers = ["build/Release/spawn-helper"];
for (const entry of await readdir(path.join(ptyOutput, "prebuilds"), {
  withFileTypes: true,
})) {
  if (entry.isDirectory() && !entry.name.startsWith("win32-"))
    helpers.push(`prebuilds/${entry.name}/spawn-helper`);
}
for (const relative of helpers) {
  await chmod(path.join(ptyOutput, relative), 0o755).catch((error) => {
    if (error.code !== "ENOENT") throw error;
  });
}
