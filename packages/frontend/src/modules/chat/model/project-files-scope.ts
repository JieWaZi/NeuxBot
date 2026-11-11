/** 保留真实 cwd 与冻结项目目录的顺序，避免多个入口重复浏览同一根。 */
export function chatFileRoots(
  workspaceDirectory: string,
  additionalWorkingDirectories: readonly string[] = [],
): string[] {
  return [
    ...new Set(
      [workspaceDirectory, ...additionalWorkingDirectories].filter(Boolean),
    ),
  ];
}

/** 空间根不作为工作目录重复展示；保留每个真实目录的原始路径。 */
export function workingDirectoryRoots(
  workspaceDirectory: string,
  additionalWorkingDirectories: readonly string[] = [],
): string[] {
  const pathKey = (path: string) => {
    const normalized = path.replaceAll("\\", "/").replace(/\/+$/, "");
    return /^[A-Za-z]:/.test(normalized)
      ? normalized.toLocaleLowerCase()
      : normalized;
  };
  const seen = new Set(workspaceDirectory ? [pathKey(workspaceDirectory)] : []);
  return additionalWorkingDirectories.filter((path) => {
    const key = pathKey(path);
    if (!path || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** 把已知根下的文件或目录身份展开为绝对路径，不访问磁盘或推断授权。 */
export function workspacePathAtRoot(
  root: string | undefined,
  path: string | undefined,
): string | undefined {
  if (!path) return undefined;
  const normalized = path.replaceAll("\\", "/");
  if (/^(?:\/|[A-Za-z]:\/)/.test(normalized)) return normalized;
  return root
    ? `${root.replaceAll("\\", "/").replace(/\/+$/, "")}/${normalized}`
    : undefined;
}

/** 文件操作按真实身份匹配；目录操作另外匹配有分隔边界的后代文件。 */
export function matchesWorkspacePath(
  candidate: string | undefined,
  target: string | undefined,
  directory = false,
): boolean {
  if (!candidate || !target) return false;
  let path = candidate.replaceAll("\\", "/");
  let scope = target.replaceAll("\\", "/").replace(/\/+$/, "");
  if (/^[A-Za-z]:\//.test(path)) {
    path = path.toLowerCase();
    scope = scope.toLowerCase();
  }
  return path === scope || (directory && path.startsWith(`${scope}/`));
}
