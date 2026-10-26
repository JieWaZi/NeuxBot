import { lstat, readFile, access } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
// 文档边界与 Git 一致：保留已跟踪文档，并检查尚未暂存的公开文档；忽略本机产物。
const candidates = new Set(
  execFileSync(
    "git",
    [
      "ls-files",
      "--cached",
      "--others",
      "--exclude-standard",
      "-z",
      "--",
      "*.md",
    ],
    {
      cwd: root,
      encoding: "utf8",
      maxBuffer: 16 * 1024 * 1024,
    },
  )
    .split("\0")
    .filter(Boolean),
);
const files = [];
for (const relative of [...candidates].sort()) {
  const target = path.join(root, relative);
  try {
    if ((await lstat(target)).isFile()) files.push(target);
  } catch (error) {
    // 待提交删除仍在 Git 索引中，不再属于当前公开文档。
    if (error.code !== "ENOENT") throw error;
  }
}
const failures = [];
for (const file of files) {
  const text = await readFile(file, "utf8");
  const relative = path.relative(root, file);
  if (/\/Users\/[\w.-]+\//.test(text))
    failures.push(`${relative}: 包含个人目录`);
  const links = [
    ...text.matchAll(/!?\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)/g),
    ...text.matchAll(/<img\b[^>]*\bsrc="([^"]+)"/g),
  ];
  for (const match of links) {
    const target = match[1]?.replace(/^<|>$/g, "").split("#")[0];
    if (!target || /^[a-z][a-z\d+.-]*:/i.test(target)) continue;
    try {
      await access(
        path.resolve(path.dirname(file), decodeURIComponent(target)),
      );
    } catch {
      failures.push(`${relative}: 链接不可用 ${target}`);
    }
  }
}
if (failures.length) throw new Error(failures.join("\n"));
console.log("文档链接、图片与个人路径检查通过");
