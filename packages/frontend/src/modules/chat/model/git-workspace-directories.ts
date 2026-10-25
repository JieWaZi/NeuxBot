import type { GithubComJieWaZiNeuxBotServerApisProjectsProjectResource as ProjectResource } from "@/shared/api/generated/types.gen";

/** 解析用于 Git 查询与分支切换的项目源目录，默认目录排在最前。 */
export function resolveGitWorkspaceDirectories(input: {
  additionalWorkingDirectories?: readonly string[];
  selectedDraftProject?: ProjectResource;
  workspaceDirectory?: string;
  /** 受管临时目录不推断其父目录的仓库。 */
  managed?: boolean;
}): string[] {
  if (input.managed) return [];
  const fromChat = (input.additionalWorkingDirectories ?? []).filter(Boolean);
  if (fromChat.length > 0) return [...fromChat];

  const sources = (input.selectedDraftProject?.sources ?? []).filter(
    (source) => source.available && source.path,
  );
  if (sources.length > 0) {
    return [...sources]
      .sort((left, right) => {
        if (left.isDefault !== right.isDefault) {
          return left.isDefault ? -1 : 1;
        }
        return 0;
      })
      .flatMap((source) => (source.path ? [source.path] : []));
  }

  return input.workspaceDirectory ? [input.workspaceDirectory] : [];
}
