import { promises as fs } from "node:fs";
import path from "node:path";

/** 账号配置由原生 CLI 消费，不作为可预览或附加到消息的工作区文件。 */
function isAccountConfiguration(target: string): boolean {
  return /(?:^|[\\/])runtime[\\/]accounts(?:[\\/]|$)/.test(target);
}
export function within(root: string, target: string): boolean {
  const relative = path.relative(root, target);
  return (
    relative === "" ||
    (!relative.startsWith(`..${path.sep}`) &&
      relative !== ".." &&
      !path.isAbsolute(relative))
  );
}
export async function canonicalDirectory(root: string): Promise<string> {
  const resolved = await fs.realpath(root);
  if (!(await fs.stat(resolved)).isDirectory())
    throw new Error("工作区路径不是目录");
  return resolved;
}
function relativeInput(value: string): string {
  if (
    path.isAbsolute(value) ||
    value.split(/[\\/]/).includes("..") ||
    value.includes("\0")
  )
    throw new Error("工作区相对路径不合法");
  return value;
}
async function resolveMissing(target: string): Promise<string> {
  try {
    return await fs.realpath(target);
  } catch (error) {
    if (
      !(error instanceof Error) ||
      !("code" in error) ||
      error.code !== "ENOENT"
    )
      throw error;
    const parent = path.dirname(target);
    if (parent === target) throw error;
    return path.join(await resolveMissing(parent), path.basename(target));
  }
}
/** 项目入口可以指向冻结的真实目录，入口内部的软链接仍须通过范围校验。 */
export async function workspacePath(
  root: string,
  relativePath: string,
  allowMissing = false,
): Promise<string> {
  const canonical = await canonicalDirectory(root);
  const relative = relativeInput(relativePath);
  const target = await (allowMissing
    ? resolveMissing(path.join(canonical, relative))
    : fs.realpath(path.join(canonical, relative)));
  if (
    isAccountConfiguration(path.join(canonical, relative)) ||
    isAccountConfiguration(target)
  )
    throw new Error("账号配置不属于工作区文件");
  if (within(canonical, target)) return target;
  const parts = relative.split(/[\\/]/).filter((p) => p && p !== ".");
  if (parts[0] === "projects") {
    const projects = await canonicalDirectory(path.join(canonical, "projects"));
    // 成员 cwd 通过相对软链接访问共享 projects/，解析后已越出 canonical。
    if (parts.length === 1) return projects;
    if (parts[1]) {
      const project = await canonicalDirectory(path.join(projects, parts[1]));
      if (within(project, target)) return target;
    }
  }
  throw new Error("工作区条目超出根目录");
}
