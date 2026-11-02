/** 在新的临时目录归档固定旧源码并复用现有依赖缓存，不修改当前工作树或安装包。 */
import { execFileSync } from "node:child_process";
import {
  access,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../../", import.meta.url));
const args = process.argv.slice(2);
const option = (name, fallback) =>
  args.includes(name) ? args[args.indexOf(name) + 1] : fallback;
const ref = option("--ref", "29b609b");
const selected = option("--out");
const baseline = selected
  ? path.resolve(selected)
  : await mkdtemp(path.join(tmpdir(), "neux-chat-ab-source-"));
if (selected) {
  try {
    await access(baseline);
    throw new Error("目标已存在；请使用新的专用临时目录，避免覆盖任何内容");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  await mkdir(baseline, { recursive: false });
}
const commit = execFileSync(
  "git",
  ["rev-parse", "--verify", `${ref}^{commit}`],
  { cwd: root, encoding: "utf8" },
).trim();
const archive = path.join(baseline, "baseline.tar");
try {
  execFileSync(
    "git",
    ["archive", "--format=tar", `--output=${archive}`, commit],
    { cwd: root },
  );
  execFileSync("tar", ["-xf", archive, "-C", baseline], {
    env: { ...process.env, LC_ALL: "C" },
  });
} finally {
  await rm(archive, { force: true });
}
await symlink(
  path.join(root, "node_modules"),
  path.join(baseline, "node_modules"),
);
const source = path.join(root, "packages/frontend/node_modules");
const target = path.join(baseline, "packages/frontend/node_modules");
await mkdir(target);
for (const entry of await readdir(source)) {
  const sourceEntry = path.join(source, entry);
  if (entry.startsWith("@") && (await lstat(sourceEntry)).isDirectory()) {
    await mkdir(path.join(target, entry));
    for (const child of await readdir(sourceEntry))
      await symlink(
        path.join(sourceEntry, child),
        path.join(target, entry, child),
      );
  } else await symlink(sourceEntry, path.join(target, entry));
}
const manifest = JSON.parse(
  await readFile(path.join(baseline, "packages/frontend/package.json"), "utf8"),
);
const store = path.join(root, "node_modules/.pnpm");
const entries = await readdir(store);
const restored = {};
for (const name of ["@assistant-ui/react-streamdown", "@streamdown/code"]) {
  const version =
    manifest.devDependencies[name] ?? manifest.dependencies?.[name];
  if (!version || !/^\d+\.\d+\.\d+$/.test(version))
    throw new Error(`旧版本未精确锁定：${name}`);
  const prefix = `${name.replace("/", "+")}@${version}`;
  const matches = entries.filter(
    (entry) =>
      entry === prefix ||
      entry.startsWith(prefix + "_") ||
      entry.startsWith(prefix + "("),
  );
  let resolved;
  for (const match of matches) {
    const candidate = path.join(store, match, "node_modules", name);
    try {
      if (
        JSON.parse(await readFile(path.join(candidate, "package.json"), "utf8"))
          .version === version
      ) {
        resolved = candidate;
        break;
      }
    } catch {
      /* 尝试同版本的下一个缓存项。 */
    }
  }
  if (!resolved)
    throw new Error(
      `现有缓存缺少 ${name}@${version}；请在旧源码目录按其锁文件准备依赖，本脚本不会联网或改当前依赖`,
    );
  const destination = path.join(target, name);
  try {
    if (!(await lstat(destination)).isSymbolicLink())
      throw new Error(`拒绝覆盖真实目录：${name}`);
    await rm(destination);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  await mkdir(path.dirname(destination), { recursive: true });
  await symlink(resolved, destination);
  restored[name] = version;
}
await writeFile(
  path.join(baseline, "benchmark-baseline.json"),
  JSON.stringify(
    {
      commit,
      restored,
      scope: "完整 Git 归档；当前依赖缓存链接与两个精确旧依赖；无产品源码替换",
    },
    null,
    2,
  ) + "\n",
);
process.stdout.write(
  JSON.stringify({ baseline, commit, restored }, null, 2) + "\n",
);
