import { i18n } from "@/shared/i18n";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useQuery } from "@tanstack/react-query";
import { FileDiff, FileText, ListTree, Waypoints } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  type ScopedGitStatus,
  listChatWorkspaceFiles,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import type { WorkspaceFile } from "@/shared/workspace/types";

import { Skeleton } from "@/shared/ui/skeleton";
import {
  gitComparisonOptions,
  gitStatusOptions,
  useChatGitRepositories,
  type ChatGitRepository,
} from "../model/chat-git";
import { ChatGitActions } from "./chat-git-actions";
import { ChatGitBranchSelector } from "./chat-git-branch-selector";
import { workspaceFilesWithGenerated } from "../model/chat-workspace-files";

const previewFileCount = 3;

type ChatSummaryProps = {
  directoryLabel?: string;
  chatId?: string;
  chatFiles: readonly WorkspaceFile[];
  gitRoots: readonly string[];
  gitBusy?: boolean;
  comparisonBaseByRepository?: Readonly<Record<string, string>>;
  onOpenChanges: (root: string, repository?: string) => void;
  onOpenFile: (file: WorkspaceFile) => void;
  onViewAllFiles: () => void;
  renderTeamMembers?: (onMemberOpened: () => void) => ReactNode;
  workspaceDirectory: string;
};

/** 会话摘要只展示可快速浏览的信息，完整文件列表仍由工作台承载。 */
export function ChatSummary({
  directoryLabel,
  chatId,
  chatFiles,
  gitRoots,
  gitBusy = false,
  comparisonBaseByRepository,
  onOpenChanges,
  onOpenFile,
  onViewAllFiles,
  renderTeamMembers,
  workspaceDirectory,
}: ChatSummaryProps) {
  const { t } = useTranslation("chat");
  const [open, setOpen] = useState(false);
  const roots = [...new Set(gitRoots.filter(Boolean))];
  const { repositories, statuses } = useChatGitRepositories(
    chatId,
    roots,
    open,
  );
  const generatedFiles = useQuery({
    queryKey: ["chat-workspace-files", chatId, workspaceDirectory],
    queryFn: () => {
      if (!chatId) throw new Error(i18n.t("common:errors.chatNotReady"));
      return listChatWorkspaceFiles(chatId, workspaceDirectory);
    },
    enabled: open && Boolean(chatId && workspaceDirectory),
  });
  const gitEvents = useWorkspaceQueryEvents(
    chatId,
    roots,
    ["scoped-git-status", "scoped-git-comparison", "scoped-git-branches"],
    open,
  );
  const workspaceEvents = useWorkspaceQueryEvents(
    chatId,
    [workspaceDirectory],
    ["chat-workspace-files"],
    open,
  );
  const files = workspaceFilesWithGenerated(
    chatFiles,
    generatedFiles.data?.generated ?? [],
    generatedFiles.data?.removedPaths ?? [],
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-expanded={open}
          aria-label={t("summary.view")}
          title={t("summary.view")}
        >
          <ListTree />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" variant="summary">
        <div className="grid min-w-0 gap-3">
          <section className="min-w-0" aria-label={t("summary.projectChanges")}>
            <div className="grid gap-2">
              {repositories.map((repository) => (
                <ChatRepositorySummary
                  key={repository.path}
                  chatId={chatId}
                  directory={repository}
                  directoryLabel={
                    repositories.length === 1 ? directoryLabel : undefined
                  }
                  discovered={
                    statuses.find(
                      (status) => status.data?.root === repository.path,
                    )?.data
                  }
                  showRepositoryPath={repositories.length > 1}
                  busy={gitBusy}
                  comparisonBase={comparisonBaseByRepository?.[repository.path]}
                  onOpenChanges={() => {
                    setOpen(false);
                    onOpenChanges(repository.root, repository.path);
                  }}
                />
              ))}
              {roots.map((root, index) =>
                statuses[index]?.isError || gitEvents.errors.has(root) ? (
                  <Button
                    key={root}
                    size="row"
                    variant="ghost"
                    onClick={() => void statuses[index]?.refetch()}
                  >
                    <FileDiff />
                    <span className="min-w-0 truncate" title={root}>
                      {root.split(/[\\/]/).filter(Boolean).at(-1)}
                    </span>
                    <span className="text-destructive type-caption">
                      {t("workspace.changesLoadError")}
                    </span>
                  </Button>
                ) : statuses[index]?.isPending ? (
                  <div
                    key={root}
                    className="grid gap-2 px-2 py-1"
                    role="status"
                    aria-label={t("workspace.changesLoading")}
                  >
                    <Skeleton className="h-4 w-2/3" />
                    <Skeleton className="h-4 w-full" />
                  </div>
                ) : null,
              )}
              {!repositories.length &&
              statuses.every((status) => status.isSuccess) ? (
                <p className="text-content-muted type-ui px-2 py-1">
                  {roots.length
                    ? t("summary.notGit")
                    : (directoryLabel ?? t("summary.noProjectDirectory"))}
                </p>
              ) : null}
            </div>
          </section>
          <section
            className="border-border-subtle min-w-0 border-t pt-3"
            aria-label={t("workspace.chatFiles")}
          >
            <h2 className="text-muted-foreground type-body mb-2">
              {t("summary.sources")}
            </h2>
            {files.length ? (
              <div className="grid">
                {files.slice(0, previewFileCount).map((file) => (
                  <Button
                    key={file.path}
                    size="row"
                    variant="ghost"
                    className="w-full min-w-0"
                    title={file.path}
                    onClick={() => {
                      setOpen(false);
                      onOpenFile(file);
                    }}
                  >
                    <WorkspaceFileTypeIcon
                      isDirectory={false}
                      name={file.name}
                      size={16}
                    />
                    <span className="min-w-0 truncate font-normal">
                      {file.name}
                    </span>
                  </Button>
                ))}
                <Button
                  size="row"
                  variant="ghost"
                  className="w-full min-w-0"
                  onClick={() => {
                    setOpen(false);
                    onViewAllFiles();
                  }}
                >
                  <Waypoints className="shrink-0" />
                  {t("summary.viewAll")}
                </Button>
              </div>
            ) : generatedFiles.isPending && chatId && workspaceDirectory ? (
              <p className="text-muted-foreground type-ui py-2">
                {t("workspace.chatFilesLoading")}
              </p>
            ) : generatedFiles.isError || workspaceEvents.error ? (
              <p className="text-destructive type-ui py-2">
                {t("workspace.chatFilesLoadError")}
              </p>
            ) : (
              <p className="text-muted-foreground type-ui flex items-center gap-3 py-2">
                <FileText className="size-icon-sm" />
                {t("workspace.noChatFiles")}
              </p>
            )}
          </section>
          {renderTeamMembers ? (
            <section
              className="border-border-subtle min-w-0 border-t pt-3"
              aria-label={t("summary.teamMembers")}
            >
              <h2 className="text-muted-foreground type-body mb-2">
                {t("summary.teamMembers")}
              </h2>
              {renderTeamMembers(() => setOpen(false))}
            </section>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ChatRepositorySummary({
  chatId,
  directory,
  directoryLabel,
  discovered,
  showRepositoryPath,
  busy,
  comparisonBase,
  onOpenChanges,
}: {
  chatId?: string;
  directory: ChatGitRepository;
  directoryLabel?: string;
  discovered?: ScopedGitStatus;
  showRepositoryPath: boolean;
  busy: boolean;
  comparisonBase?: string;
  onOpenChanges: () => void;
}) {
  const { t } = useTranslation("chat");
  const { root, path: repository } = directory;
  const status = useQuery({
    ...gitStatusOptions(chatId, root, repository),
    enabled: !discovered,
  });
  const snapshot = discovered ?? status.data;
  const comparison = useQuery({
    ...gitComparisonOptions(chatId, root, repository, comparisonBase),
    enabled: Boolean(snapshot),
  });
  const entries = comparison.data?.entries;
  const counts = entries?.reduce(
    (sum, entry) => ({
      added: sum.added + entry.addedLines,
      removed: sum.removed + entry.deletedLines,
      partial: sum.partial || Boolean(entry.lineCountUnavailable),
    }),
    { added: 0, removed: 0, partial: Boolean(comparison.data?.truncated) },
  );
  const base = comparison.data?.baseRef;
  const name =
    directoryLabel ??
    repository.split(/[\\/]/).filter(Boolean).at(-1) ??
    repository;
  const target = { chatId, root, repository };
  return (
    <div className="min-w-0">
      {showRepositoryPath ? (
        <h2
          className="text-content-muted type-caption truncate px-2"
          title={repository}
        >
          {name}
        </h2>
      ) : null}
      {snapshot ? (
        <>
          <ChatGitBranchSelector
            target={target}
            status={snapshot}
            disabled={busy}
            showRepositoryPath={showRepositoryPath}
          />
          <ChatGitActions target={target} status={snapshot} disabled={busy} />
        </>
      ) : null}
      <Button
        size="row"
        variant="ghost"
        className="w-full min-w-0"
        aria-label={t("summary.directoryChanges", { directory: name })}
        title={
          base && base !== "HEAD"
            ? t("summary.git.compareAgainst", { base })
            : repository
        }
        onClick={onOpenChanges}
      >
        <FileDiff />
        <span className="min-w-0 flex-1 truncate text-left">
          {t("summary.changes")}
          {base && base !== "HEAD" ? (
            <span className="text-content-muted type-caption ml-1">
              {t("summary.git.compareAgainst", { base })}
            </span>
          ) : null}
        </span>
        {comparison.isError || status.isError ? (
          <span className="text-destructive type-caption">
            {t("workspace.changesLoadError")}
          </span>
        ) : counts ? (
          <span className="flex shrink-0 gap-1 font-normal tabular-nums">
            <span className="text-success">+{counts.added}</span>
            <span className="text-destructive">-{counts.removed}</span>
            {counts.partial ? (
              <span aria-label={t("workspace.git.tooManyFiles")}>…</span>
            ) : null}
          </span>
        ) : (
          <div role="status" aria-label={t("workspace.changesLoading")}>
            <Skeleton className="h-3 w-10" />
          </div>
        )}
      </Button>
    </div>
  );
}
