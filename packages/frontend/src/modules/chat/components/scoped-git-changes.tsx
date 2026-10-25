import { useWorkspaceConfirmation } from "./use-workspace-confirmation";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useQuery } from "@tanstack/react-query";
import { FileDiff, Folders, GitBranch, History, RefreshCw } from "lucide-react";
import { DiffModeEnum } from "@git-diff-view/react";
import { lazy, Suspense, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  commitScopedGitFiles,
  discardScopedGitFile,
  getScopedGitCommitDiff,
  getScopedGitDiff,
  getScopedGitLog,
  getScopedGitStatus,
  stageScopedGitFile,
  type ScopedGitStatus,
  unstageScopedGitFile,
} from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import { EmptyState } from "@/shared/ui/empty-state";
import { Input } from "@/shared/ui/input";
import { SearchField } from "@/shared/ui/search-field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Spinner } from "@/shared/ui/spinner";
import { GitPatchDiffModeToggle } from "./git-patch-preview";
import { WorkspaceDetailSplit } from "./workspace-detail-split";
import { ScopedGitFileList, type GitSelection } from "./scoped-git-file-list";

type GitEntry = ScopedGitStatus["entries"][number];
type Repository = { root: string; path: string };
type Selection = GitSelection;
const GitPatchPreview = lazy(() =>
  import("./git-patch-preview").then((module) => ({
    default: module.GitPatchPreview,
  })),
);
export type ScopedGitChangesProps = {
  active?: boolean;
  activePath?: string;
  query: string;
  onQueryChange?: (query: string) => void;
  preferredRoot?: string;
  rootPath: string;
  scopeChatId?: string;
  sourceRoots?: readonly string[];
};

/** 在当前 Chat 授权范围内组合仓库、暂存区、工作区与提交历史。 */
export function ScopedGitChanges({
  active = true,
  activePath,
  query,
  onQueryChange,
  preferredRoot,
  rootPath,
  scopeChatId,
  sourceRoots = [],
}: ScopedGitChangesProps) {
  const { t } = useTranslation("chat");
  const confirmation = useWorkspaceConfirmation();
  const [chosen, setChosen] = useState<string>();
  const [selection, setSelection] = useState<Selection>();
  const [commitHash, setCommitHash] = useState<string>();
  const [message, setMessage] = useState("");
  const [mode, setMode] = useState<"changes" | "history">("changes");
  const [listOpen, setListOpen] = useState(true);
  const [diffMode, setDiffMode] = useState(DiffModeEnum.Unified);
  const [localQuery, setLocalQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const roots = [...new Set([rootPath, ...sourceRoots].filter(Boolean))];
  const statuses = useQuery({
    queryKey: ["scoped-git-status", scopeChatId, roots],
    queryFn: () => {
      if (!scopeChatId) throw new Error(t("workspace.git.scopeUnavailable"));
      return Promise.all(
        roots.map(async (root) => ({
          root,
          status: await getScopedGitStatus(scopeChatId, root),
        })),
      );
    },
    enabled: Boolean(active && scopeChatId && roots.length),
  });
  const repositories: Repository[] = [
    ...new Map(
      (statuses.data ?? [])
        .flatMap(({ root, status }) =>
          (status.repositories ?? []).map((repository) => ({
            root,
            path: repository,
          })),
        )
        .map((item) => [item.path, item] as const),
    ).values(),
  ];
  const selected =
    repositories.find((item) => item.path === chosen) ??
    repositories.find((item) => item.root === preferredRoot) ??
    repositories[0];
  const detail = useQuery({
    queryKey: [
      "scoped-git-detail",
      scopeChatId,
      selected?.root,
      selected?.path,
    ],
    queryFn: () => {
      if (!scopeChatId || !selected)
        throw new Error(t("workspace.git.repositoryUnavailable"));
      return getScopedGitStatus(scopeChatId, selected.root, selected.path);
    },
    enabled: Boolean(active && scopeChatId && selected),
  });
  const workspaceEvents = useWorkspaceQueryEvents(
    scopeChatId,
    roots,
    [
      "scoped-git-status",
      "scoped-git-detail",
      "scoped-git-diff",
      "scoped-git-log",
      "scoped-git-commit-diff",
    ],
    active,
  );
  const status = detail.data;
  const visibleSelection = status?.entries.some(
    (entry) => entry.path === selection?.path,
  )
    ? selection
    : undefined;
  const searchQuery = onQueryChange ? query : localQuery;
  const filter = searchQuery.trim().toLocaleLowerCase();
  const staged = (status?.entries ?? []).filter(
    (entry) => isStaged(entry) && matches(entry, filter),
  );
  const unstaged = (status?.entries ?? []).filter(
    (entry) => isUnstaged(entry) && matches(entry, filter),
  );
  const preview = useQuery({
    queryKey: [
      "scoped-git-diff",
      scopeChatId,
      selected?.root,
      selected?.path,
      visibleSelection,
    ],
    queryFn: () => {
      if (!scopeChatId || !selected || !visibleSelection)
        throw new Error(t("workspace.git.fileUnavailable"));
      return getScopedGitDiff(
        scopeChatId,
        selected.root,
        selected.path,
        visibleSelection.path,
        visibleSelection.staged,
      );
    },
    enabled: Boolean(
      active &&
      mode === "changes" &&
      scopeChatId &&
      selected &&
      visibleSelection,
    ),
  });
  const history = useQuery({
    queryKey: ["scoped-git-log", scopeChatId, selected?.root, selected?.path],
    queryFn: () => {
      if (!scopeChatId || !selected)
        throw new Error(t("workspace.git.repositoryUnavailable"));
      return getScopedGitLog(scopeChatId, selected.root, selected.path);
    },
    enabled: Boolean(active && mode === "history" && scopeChatId && selected),
  });
  const historyPatch = useQuery({
    queryKey: [
      "scoped-git-commit-diff",
      scopeChatId,
      selected?.root,
      selected?.path,
      commitHash,
    ],
    queryFn: () => {
      if (!scopeChatId || !selected || !commitHash)
        throw new Error(t("workspace.git.commitUnavailable"));
      return getScopedGitCommitDiff(
        scopeChatId,
        selected.root,
        selected.path,
        commitHash,
      );
    },
    enabled: Boolean(
      active && mode === "history" && scopeChatId && selected && commitHash,
    ),
  });

  const apply = async (action: () => Promise<unknown>, committed = false) => {
    setBusy(true);
    setError("");
    try {
      const result = await action();
      if (result !== false) {
        if (committed) setMessage("");
        await Promise.all([
          detail.refetch(),
          statuses.refetch(),
          ...(mode === "history" ? [history.refetch()] : []),
          ...(visibleSelection ? [preview.refetch()] : []),
        ]);
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  };

  if (!rootPath || !scopeChatId)
    return <GitState title={t("workspace.noProjectDirectory")} />;
  if (statuses.isPending)
    return <GitState title={t("workspace.changesLoading")} loading />;
  if (statuses.isError)
    return (
      <GitState
        title={t("workspace.changesLoadError")}
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void statuses.refetch()}
          >
            <RefreshCw />
            {t("workspace.retry")}
          </Button>
        }
      />
    );
  if (!selected)
    return (
      <GitState
        title={workspaceEvents.error?.message ?? t("workspace.noGitRepository")}
      />
    );
  if (detail.isPending)
    return <GitState title={t("workspace.changesLoading")} loading />;
  if (detail.isError || !status)
    return <GitState title={t("workspace.changesLoadError")} />;

  const hasPreview =
    mode === "changes" ? Boolean(visibleSelection) : Boolean(commitHash);
  return (
    <div className="bg-surface-scene @container flex h-full min-h-0 min-w-0 flex-col">
      {confirmation.dialog}
      <header className="border-border-subtle h-row-navigation flex min-w-0 shrink-0 items-center gap-2 border-b px-3">
        {repositories.length > 1 ? (
          <Select
            value={selected.path}
            onValueChange={(value) => {
              setChosen(value);
              setSelection(undefined);
              setCommitHash(undefined);
              setError("");
            }}
          >
            <SelectTrigger
              aria-label={t("workspace.gitRepository")}
              size="sm"
              className="max-w-44 min-w-0"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {repositories.map((repository) => (
                <SelectItem key={repository.path} value={repository.path}>
                  {repository.path.split(/[\\/]/).at(-1)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <span
            className="type-ui flex min-w-0 items-center gap-1.5"
            title={selected.path}
          >
            <GitBranch className="text-content-muted size-icon-sm shrink-0" />
            <span className="truncate">{status.branch ?? "HEAD"}</span>
          </span>
        )}
        <Badge variant={staged.length ? "success" : "secondary"}>
          {t("workspace.git.stagedCount", { count: staged.length })}
        </Badge>
        <Badge variant={unstaged.length ? "warning" : "secondary"}>
          {t("workspace.git.changedCount", { count: unstaged.length })}
        </Badge>
        <span className="min-w-0 flex-1" />
        <GitPatchDiffModeToggle mode={diffMode} onModeChange={setDiffMode} />
        <Button
          aria-label={t("workspace.changes")}
          title={t("workspace.changes")}
          aria-pressed={mode === "changes"}
          size="icon-sm"
          variant={mode === "changes" ? "secondary" : "ghost"}
          onClick={() => setMode("changes")}
        >
          <FileDiff />
        </Button>
        <Button
          aria-label={t("workspace.git.history")}
          title={t("workspace.git.history")}
          aria-pressed={mode === "history"}
          size="icon-sm"
          variant={mode === "history" ? "secondary" : "ghost"}
          onClick={() => setMode("history")}
        >
          <History />
        </Button>
        <Button
          aria-label={
            listOpen ? t("workspace.git.hideList") : t("workspace.git.showList")
          }
          title={
            listOpen ? t("workspace.git.hideList") : t("workspace.git.showList")
          }
          aria-pressed={listOpen}
          size="icon-sm"
          variant="ghost"
          onClick={() => setListOpen((current) => !current)}
        >
          <Folders />
        </Button>
        <Button
          aria-label={t("workspace.retry")}
          title={t("workspace.retry")}
          size="icon-sm"
          variant="ghost"
          onClick={() => {
            workspaceEvents.retry();
            void Promise.all([statuses.refetch(), detail.refetch()]);
          }}
        >
          <RefreshCw />
        </Button>
      </header>
      {status.truncated ? (
        <p className="text-warning type-caption border-border-subtle border-b px-3 py-2">
          {t("workspace.git.tooManyFiles")}
        </p>
      ) : null}
      {error || workspaceEvents.error ? (
        <p
          className="text-destructive type-caption border-border-subtle border-b px-3 py-2"
          role="alert"
        >
          {error || workspaceEvents.error?.message}
        </p>
      ) : null}
      <WorkspaceDetailSplit
        sidebarOpen={listOpen}
        resizeLabel={t("workbench.resizeSplit")}
        main={
          <main
            aria-label={
              mode === "changes"
                ? t("workspace.git.diffPreview")
                : t("workspace.git.historyPreview")
            }
            className="flex min-h-0 min-w-0 flex-col overflow-hidden"
          >
            {hasPreview ? (
              <>
                <div className="min-h-0 flex-1 overflow-hidden px-2 py-2">
                  {mode === "changes" ? (
                    preview.isPending ? (
                      <Spinner />
                    ) : preview.isError ? (
                      <p className="text-destructive type-caption">
                        {t("workspace.git.diffLoadError")}
                      </p>
                    ) : preview.data && visibleSelection ? (
                      <Suspense fallback={<Spinner />}>
                        <GitPatchPreview
                          diffMode={diffMode}
                          path={visibleSelection.path}
                          patch={preview.data}
                          fullHeight
                        />
                      </Suspense>
                    ) : (
                      <p className="text-content-muted type-caption px-2 py-3">
                        {visibleSelection?.staged
                          ? t("workspace.git.stagedDiffEmpty")
                          : t("workspace.git.textDiffEmpty")}
                      </p>
                    )
                  ) : historyPatch.isPending ? (
                    <Spinner />
                  ) : historyPatch.isError ? (
                    <p className="text-destructive type-caption">
                      {t("workspace.git.commitDiffLoadError")}
                    </p>
                  ) : historyPatch.data && commitHash ? (
                    <Suspense fallback={<Spinner />}>
                      <GitPatchPreview
                        diffMode={diffMode}
                        path={commitHash}
                        patch={historyPatch.data}
                        fullHeight
                      />
                    </Suspense>
                  ) : (
                    <p className="text-content-muted type-caption px-2 py-3">
                      {t("workspace.git.commitDiffEmpty")}
                    </p>
                  )}
                </div>
              </>
            ) : (
              <EmptyState
                icon={mode === "changes" ? FileDiff : History}
                title={
                  mode === "changes"
                    ? t("workspace.git.chooseFile")
                    : t("workspace.git.chooseCommit")
                }
                className="flex-1"
              />
            )}
          </main>
        }
        sidebar={
          <aside
            aria-label={
              mode === "changes"
                ? t("workspace.git.changedFiles")
                : t("workspace.git.commitHistory")
            }
            className="bg-surface-panel flex h-full min-h-0 min-w-0 flex-col"
          >
            {mode === "changes" ? (
              <>
                <div className="p-2">
                  <SearchField
                    aria-label={t("workspace.searchChanges")}
                    placeholder={t("workspace.searchChangesPlaceholder")}
                    value={searchQuery}
                    onChange={(event) =>
                      onQueryChange
                        ? onQueryChange(event.target.value)
                        : setLocalQuery(event.target.value)
                    }
                    width="full"
                  />
                </div>
                <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto px-2 py-2">
                  <ScopedGitFileList
                    title={t("workspace.git.staged")}
                    entries={staged}
                    staged
                    activePath={activePath}
                    selection={visibleSelection}
                    onSelect={(next) => {
                      setSelection(next);
                      setCommitHash(undefined);
                    }}
                    busy={busy}
                    onAction={(relativePath) =>
                      void apply(() =>
                        unstageScopedGitFile(
                          scopeChatId,
                          selected.root,
                          selected.path,
                          relativePath,
                        ),
                      )
                    }
                  />
                  <ScopedGitFileList
                    title={t("workspace.git.changed")}
                    entries={unstaged}
                    staged={false}
                    activePath={activePath}
                    selection={visibleSelection}
                    onSelect={(next) => {
                      setSelection(next);
                      setCommitHash(undefined);
                    }}
                    busy={busy}
                    onAction={(relativePath) =>
                      void apply(() =>
                        stageScopedGitFile(
                          scopeChatId,
                          selected.root,
                          selected.path,
                          relativePath,
                        ),
                      )
                    }
                    onDiscard={(relativePath) => {
                      void confirmation
                        .confirm({ kind: "discard", path: relativePath })
                        .then((confirmed) => {
                          if (confirmed)
                            void apply(() =>
                              discardScopedGitFile(
                                scopeChatId,
                                selected.root,
                                selected.path,
                                relativePath,
                              ),
                            );
                        });
                    }}
                  />
                  {status.entries.length === 0 ? (
                    <GitState title={t("workspace.noWorkspaceChanges")} />
                  ) : null}
                  {status.entries.length > 0 &&
                  staged.length + unstaged.length === 0 ? (
                    <GitState title={t("workspace.noMatchingChanges")} />
                  ) : null}
                </div>
                <div className="border-border-subtle flex shrink-0 flex-col gap-2 border-t p-2">
                  <Input
                    aria-label={t("workspace.git.commitMessage")}
                    size="sm"
                    placeholder={t("workspace.git.commitMessage")}
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                    maxLength={4096}
                  />
                  <Button
                    size="sm"
                    disabled={busy || !staged.length || !message.trim()}
                    onClick={() =>
                      void apply(
                        () =>
                          commitScopedGitFiles(
                            scopeChatId,
                            selected.root,
                            selected.path,
                            message,
                          ),
                        true,
                      )
                    }
                  >
                    {t("workspace.git.commitStaged")}
                  </Button>
                </div>
              </>
            ) : (
              <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto p-2">
                {history.isPending ? <Spinner /> : null}
                {history.data?.map((entry) => (
                  <button
                    key={entry.hashFull}
                    type="button"
                    aria-current={
                      commitHash === entry.hashFull ? "true" : undefined
                    }
                    className={cn(
                      "type-caption hover:bg-muted min-h-row-default block w-full rounded-sm px-2 py-1 text-left",
                      commitHash === entry.hashFull && "bg-selection-surface",
                    )}
                    onClick={() => {
                      setCommitHash(entry.hashFull);
                      setSelection(undefined);
                    }}
                  >
                    <span className="text-link font-mono">{entry.hash}</span>{" "}
                    {entry.subject}
                    <span className="text-content-muted block truncate">
                      {entry.author} · {entry.date}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </aside>
        }
      />
    </div>
  );
}

function isStaged(entry: GitEntry): boolean {
  return entry.xy[0] !== " " && entry.xy[0] !== "?";
}

function isUnstaged(entry: GitEntry): boolean {
  return entry.xy === "??" || (entry.xy[1] !== " " && entry.xy[1] !== "?");
}

function matches(entry: GitEntry, filter: string): boolean {
  return !filter || entry.path.toLocaleLowerCase().includes(filter);
}

function GitState({
  title,
  loading,
  action,
}: {
  title: string;
  loading?: boolean;
  action?: ReactNode;
}) {
  return (
    <div
      className="text-muted-foreground type-body flex min-h-32 flex-col items-center justify-center gap-3 px-4 text-center"
      role={loading ? "status" : undefined}
    >
      {loading ? <Spinner /> : <FileDiff className="size-icon-xl" />}
      <span>{title}</span>
      {action}
    </div>
  );
}
