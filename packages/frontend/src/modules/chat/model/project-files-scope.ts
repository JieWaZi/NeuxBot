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
