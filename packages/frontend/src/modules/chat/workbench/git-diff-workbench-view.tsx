import { DiffModeEnum } from "@git-diff-view/react";
import { useQuery } from "@tanstack/react-query";
import { FileDiff, RefreshCw } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import { useTranslation } from "react-i18next";

import { getScopedGitCommitDiff, getScopedGitDiff } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import { GitPatchDiffModeToggle } from "../components/git-patch-preview";
import type { WorkbenchTab } from "./workbench-model";

const GitPatchPreview = lazy(() =>
  import("../components/git-patch-preview").then((module) => ({
    default: module.GitPatchPreview,
  })),
);
/** 审查标签持有补丁身份；每次显示都重新通过 Main 校验 Chat 和仓库。 */
export function GitDiffWorkbenchView({
  chatId,
  tab,
}: {
  chatId?: string;
  tab: WorkbenchTab;
}) {
  const { t } = useTranslation("chat");
  const [diffMode, setDiffMode] = useState(DiffModeEnum.Unified);
  const sourceRoot = tab.rootPath;
  const repository = tab.gitRepository;
  const valid = Boolean(
    chatId && sourceRoot && repository && (tab.path || tab.gitCommitHash),
  );
  const patch = useQuery({
    queryKey: [
      "workbench-git-diff",
      chatId,
      sourceRoot,
      repository,
      tab.path,
      tab.gitStaged,
      tab.gitCommitHash,
    ],
    queryFn: () => {
      if (!chatId || !sourceRoot || !repository)
        throw new Error(t("workspace.git.repositoryUnavailable"));
      if (tab.gitCommitHash)
        return getScopedGitCommitDiff(
          chatId,
          sourceRoot,
          repository,
          tab.gitCommitHash,
        );
      if (!tab.path) throw new Error(t("workspace.git.fileUnavailable"));
      return getScopedGitDiff(
        chatId,
        sourceRoot,
        repository,
        tab.path,
        Boolean(tab.gitStaged),
      );
    },
    enabled: valid,
  });

  return (
    <section
      className="flex h-full min-h-0 min-w-0 flex-col"
      aria-label={tab.title}
    >
      <div className="border-border-subtle flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <FileDiff className="text-muted-foreground size-icon-sm shrink-0" />
        <span className="type-ui min-w-0 flex-1 truncate" title={tab.title}>
          {tab.title}
        </span>
        <GitPatchDiffModeToggle mode={diffMode} onModeChange={setDiffMode} />
        <Button
          aria-label={t("workspace.git.refreshDiff")}
          size="icon-sm"
          variant="ghost"
          disabled={!valid}
          onClick={() => void patch.refetch()}
        >
          <RefreshCw />
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {!valid ? (
          <p className="text-muted-foreground type-body p-4">
            {t("workspace.git.diffUnavailable")}
          </p>
        ) : patch.isPending ? (
          <div className="grid h-full place-items-center">
            <Spinner />
          </div>
        ) : patch.isError ? (
          <p className="text-destructive type-body p-4" role="alert">
            {patch.error instanceof Error
              ? patch.error.message
              : t("workspace.git.diffLoadError")}
          </p>
        ) : patch.data ? (
          <Suspense fallback={<Spinner />}>
            <GitPatchPreview
              diffMode={diffMode}
              onDiffModeChange={setDiffMode}
              path={tab.path ?? tab.gitCommitHash ?? tab.title}
              patch={patch.data}
              fullHeight
            />
          </Suspense>
        ) : (
          <p className="text-muted-foreground type-body p-4">
            {t("workspace.git.diffEmpty")}
          </p>
        )}
      </div>
    </section>
  );
}
