import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlignJustify,
  ChevronsDownUp,
  ChevronsUpDown,
  Columns2,
  FileDiff,
  FlaskConicalOff,
  Folders,
  Pilcrow,
  RefreshCw,
  WrapText,
} from "lucide-react";
import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { observeViewportActivation } from "@/shared/lib/viewport-activation";
import {
  discardScopedGitFile,
  getScopedGitLog,
  listWorkspaceGitBranches,
  stageScopedGitFile,
  unstageScopedGitFile,
  type ScopedGitStatus,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { SearchField } from "@/shared/ui/search-field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  gitComparisonOptions,
  gitStatusOptions,
  invalidateChatGit,
  useChatGitRepositories,
  type ChatGitRepository,
} from "../model/chat-git";
import type { DiffMode } from "./git-patch-preview";
import {
  diffFileKey,
  DiffLineCounts,
  ScopedGitChangedTree,
  ScopedGitFileList,
  type DiffEntry,
  type DiffGroup,
} from "./scoped-git-file-list";
import { useWorkspaceConfirmation } from "./use-workspace-confirmation";
import { WorkspaceDetailSplit } from "./workspace-detail-split";

const GitPatchBody = lazy(() =>
  import("./git-patch-preview").then(({ GitPatchBody }) => ({
    default: GitPatchBody,
  })),
);

const gitQueryPrefixes = [
  "scoped-git-status",
  "scoped-git-branches",
  "scoped-git-comparison",
  "scoped-git-log",
];
const automaticBase = ":auto";
type DiffSource = "changes" | "uncommitted" | "history" | "index";

export type ScopedGitChangesProps = {
  active?: boolean;
  activePath?: string;
  comparisonBaseByRepository?: Readonly<Record<string, string>>;
  onComparisonBaseChange?: (repository: string, base: string) => void;
  query: string;
  onQueryChange?: (query: string) => void;
  onOpenFile?: (root: string, path: string) => void;
  preferredRoot?: string;
  preferredRepository?: string;
  rootPath: string;
  scopeChatId?: string;
  sourceRoots?: readonly string[];
};

/** 在同一个工作台中审查仓库净改动、暂存状态和单次提交。 */
export function ScopedGitChanges({
  active = true,
  activePath,
  comparisonBaseByRepository,
  onComparisonBaseChange,
  query,
  onQueryChange,
  onOpenFile,
  preferredRoot,
  preferredRepository,
  rootPath,
  scopeChatId,
  sourceRoots = [],
}: ScopedGitChangesProps) {
  const { t } = useTranslation("chat");
  const roots = [...new Set([rootPath, ...sourceRoots].filter(Boolean))];
  const { repositories, statuses } = useChatGitRepositories(
    scopeChatId,
    roots,
    active,
  );
  const [chosen, setChosen] = useState<string>();
  const selected =
    repositories.find(({ path }) => path === chosen) ??
    repositories.find(({ path }) => path === preferredRepository) ??
    repositories.find(({ root }) => root === preferredRoot) ??
    repositories[0];
  const workspaceEvents = useWorkspaceQueryEvents(
    scopeChatId,
    roots,
    gitQueryPrefixes,
    active,
  );

  if (!roots.length)
    return <GitState title={t("workspace.noProjectDirectory")} />;
  if (!selected) {
    if (statuses.some((status) => status.isPending))
      return <DiffLoading title={t("workspace.changesLoading")} />;
    if (statuses.some((status) => status.isError))
      return (
        <GitState title={t("workspace.changesLoadError")}>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              workspaceEvents.retry();
              for (const status of statuses)
                if (status.isError) void status.refetch();
            }}
          >
            {t("workspace.retry")}
          </Button>
        </GitState>
      );
    return (
      <GitState
        title={workspaceEvents.error?.message ?? t("workspace.noGitRepository")}
      />
    );
  }
  return (
    <RepositoryDiff
      key={selected.path}
      active={active}
      activePath={activePath}
      chatId={scopeChatId}
      comparisonBase={comparisonBaseByRepository?.[selected.path]}
      onComparisonBaseChange={onComparisonBaseChange}
      onOpenFile={onOpenFile}
      onQueryChange={onQueryChange}
      onRepositoryChange={setChosen}
      query={query}
      repositories={repositories}
      repository={selected}
      discoveredStatus={
        statuses.find((status) => status.data?.root === selected.path)?.data
      }
      watchError={workspaceEvents.error}
      retryWatch={workspaceEvents.retry}
    />
  );
}

function RepositoryDiff({
  active,
  activePath,
  chatId,
  comparisonBase: sharedBase,
  onComparisonBaseChange,
  onOpenFile,
  onQueryChange,
  onRepositoryChange,
  query,
  repositories,
  repository,
  discoveredStatus,
  watchError,
  retryWatch,
}: {
  active: boolean;
  activePath?: string;
  chatId?: string;
  comparisonBase?: string;
  onComparisonBaseChange?: (repository: string, base: string) => void;
  onOpenFile?: (root: string, path: string) => void;
  onQueryChange?: (query: string) => void;
  onRepositoryChange: (path: string) => void;
  query: string;
  repositories: ChatGitRepository[];
  repository: ChatGitRepository;
  discoveredStatus?: ScopedGitStatus;
  watchError?: Error;
  retryWatch: () => void;
}) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const confirmation = useWorkspaceConfirmation();
  const [source, setSource] = useState<DiffSource>("changes");
  const [localBase, setLocalBase] = useState<string>();
  const comparisonBase =
    (onComparisonBaseChange ? sharedBase : localBase) || undefined;
  const [baseOpen, setBaseOpen] = useState(false);
  const [selectedCommit, setSelectedCommit] = useState("");
  const [diffMode, setDiffMode] = useState<DiffMode>("unified");
  const [wrap, setWrap] = useState(false);
  const [ignoreWhitespace, setIgnoreWhitespace] = useState(false);
  const [hideTests, setHideTests] = useState(false);
  const [treeOpen, setTreeOpen] = useState(true);
  const [localQuery, setLocalQuery] = useState("");
  const [expansion, setExpansion] = useState({
    all: false,
    exceptions: new Set<string>(),
  });
  const [focused, setFocused] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const main = useRef<HTMLElement>(null);

  const { root, path: repo } = repository;
  const status = useQuery({
    ...gitStatusOptions(chatId, root, repo),
    enabled: active && source === "index" && !discoveredStatus,
  });
  const observedStatus = discoveredStatus ?? status.data;
  const branches = useQuery({
    queryKey: ["scoped-git-branches", chatId, root, repo],
    queryFn: () => listWorkspaceGitBranches(root, { chatId, repository: repo }),
    enabled: active && source === "changes" && baseOpen,
  });
  const historyBase = comparisonBase ?? automaticBase;
  const history = useQuery({
    queryKey: ["scoped-git-log", chatId, root, repo, historyBase],
    queryFn: () => getScopedGitLog(chatId, root, repo, 100, 0, historyBase),
    enabled: active && source === "history",
  });
  const commit = history.data?.some(
    ({ hashFull }) => hashFull === selectedCommit,
  )
    ? selectedCommit
    : "";
  let bases: Array<string | undefined>;
  switch (source) {
    case "uncommitted":
      bases = ["HEAD"];
      break;
    case "index":
      bases = [":staged", ":unstaged"];
      break;
    case "history":
      bases = commit ? [":commit:" + commit] : [];
      break;
    case "changes":
      bases = [comparisonBase];
      break;
  }
  const comparisons = useQueries({
    queries: bases.map((base) => ({
      ...gitComparisonOptions(
        chatId,
        root,
        repo,
        base,
        undefined,
        ignoreWhitespace,
      ),
      enabled: active,
    })),
  });
  const groups: DiffGroup[] = (() => {
    const search = (onQueryChange ? query : localQuery)
      .trim()
      .toLocaleLowerCase();
    return comparisons.map(({ data }, index) => ({
      id: bases[index] ?? automaticBase,
      title:
        source === "index"
          ? t(
              index === 0
                ? "workspace.git.staged"
                : "workspace.git.workingTree",
            )
          : undefined,
      files: (data?.entries ?? []).filter(
        ({ path, oldPath }) =>
          (!hideTests || !isTestPath(path)) &&
          (!search ||
            path.toLocaleLowerCase().includes(search) ||
            oldPath?.toLocaleLowerCase().includes(search)),
      ),
    }));
  })();
  const counts = groups
    .flatMap(({ files }) => files)
    .reduce(
      (sum, entry) => ({
        added: sum.added + entry.addedLines,
        deleted: sum.deleted + entry.deletedLines,
        unavailable: sum.unavailable || entry.lineCountUnavailable,
      }),
      { added: 0, deleted: 0, unavailable: false as boolean | undefined },
    );
  const anyExpanded = groups.some(({ id, files }) =>
    files.some(
      ({ path }) =>
        expansion.all !== expansion.exceptions.has(diffFileKey(id, path)),
    ),
  );
  const hasFiles = groups.some(({ files }) => files.length);
  const initialLoading =
    comparisons.some((result) => result.isPending && result.isFetching) ||
    (source === "history" && history.isPending && history.isFetching);
  const comparisonError = comparisons.find(
    (result) => !result.data && result.isError,
  )?.error;
  const loadError =
    comparisonError ??
    (source === "history" && !history.data && history.isError
      ? history.error
      : undefined);
  const comparisonTruncated = comparisons.some(({ data }) => data?.truncated);
  const autoLabel = queryClient.getQueryData(
    gitComparisonOptions(
      chatId,
      root,
      repo,
      undefined,
      undefined,
      ignoreWhitespace,
    ).queryKey,
  )?.baseRef;
  const automaticLabel =
    autoLabel === "HEAD"
      ? t("workspace.git.automaticUncommitted")
      : autoLabel
        ? t("workspace.git.automaticBase", { base: autoLabel })
        : t("workspace.git.automatic");
  const pendingRefresh =
    comparisons.some((result) => result.isFetching) ||
    (source === "history" && history.isFetching);

  const resetExpansion = () => {
    setExpansion({ all: false, exceptions: new Set() });
    setFocused(undefined);
  };
  const toggle = (key: string) => {
    setExpansion(({ all, exceptions }) => {
      const next = new Set(exceptions);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return { all, exceptions: next };
    });
  };
  const jump = (key: string) => {
    const row = [
      ...(main.current?.querySelectorAll<HTMLElement>("[data-git-file]") ?? []),
    ].find(({ dataset }) => dataset.gitFile === key);
    if (!row) return;
    setFocused(key);
    if (expansion.all === expansion.exceptions.has(key)) toggle(key);
    row.scrollIntoView?.({ block: "start" });
    row
      .querySelector<HTMLButtonElement>("[data-diff-toggle]")
      ?.focus({ preventScroll: true });
  };
  const refresh = () => {
    retryWatch();
    void invalidateChatGit(queryClient, root, repo);
  };
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      if ((await action()) !== false)
        await invalidateChatGit(queryClient, root, repo);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      await invalidateChatGit(queryClient, root, repo);
    } finally {
      setBusy(false);
    }
  };
  const observeIndex = (source: string) => {
    const displayedBranch = comparisons[bases.indexOf(source)]?.data?.branch;
    const expectedBranch =
      displayedBranch === "HEAD" ? "HEAD (no branch)" : displayedBranch;
    const snapshot =
      observedStatus?.branch === expectedBranch ? observedStatus : undefined;
    return {
      expectedBranch,
      expectedHead: snapshot?.head,
      expectedIndex: snapshot?.index,
    };
  };
  const discard = (path: string) => {
    const observation = observeIndex(":unstaged");
    void run(async () => {
      const accepted = await confirmation.confirm({ kind: "discard", path });
      return (
        accepted && discardScopedGitFile(chatId, root, repo, path, observation)
      );
    });
  };

  return (
    <div className="bg-surface-scene flex h-full min-h-0 min-w-0 flex-col">
      {confirmation.dialog}
      <header className="border-border-subtle flex shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-2 py-1">
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          <Select
            value={source}
            onValueChange={(value: DiffSource) => {
              setSource(value);
              resetExpansion();
            }}
          >
            <SelectTrigger aria-label={t("workspace.git.diffSource")} size="sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="changes">
                {t("workspace.git.sourceChanges")}
              </SelectItem>
              <SelectItem value="uncommitted">
                {t("workspace.git.sourceUncommitted")}
              </SelectItem>
              <SelectItem value="history">
                {t("workspace.git.commitHistory")}
              </SelectItem>
              <SelectItem value="index">
                {t("workspace.git.sourceIndex")}
              </SelectItem>
            </SelectContent>
          </Select>
          {repositories.length > 1 ? (
            <Select value={repo} onValueChange={onRepositoryChange}>
              <SelectTrigger
                aria-label={t("workspace.gitRepository")}
                size="sm"
                className="max-w-44 min-w-0"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {repositories.map(({ path }) => (
                  <SelectItem key={path} value={path} title={path}>
                    {repositories.some(
                      (other) =>
                        other.path !== path &&
                        repositoryName(other.path) === repositoryName(path),
                    )
                      ? path
                      : repositoryName(path)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <span
              className="type-ui text-content-muted max-w-36 truncate px-1.5"
              title={repo}
            >
              {repositoryName(repo)}
            </span>
          )}
          {source === "changes" ? (
            <Select
              value={comparisonBase ?? automaticBase}
              onOpenChange={setBaseOpen}
              onValueChange={(value) => {
                const base = value === automaticBase ? "" : value;
                if (onComparisonBaseChange) onComparisonBaseChange(repo, base);
                else setLocalBase(base);
                resetExpansion();
              }}
            >
              <SelectTrigger
                aria-label={t("workspace.git.diffBase")}
                size="sm"
                className="max-w-56 min-w-0"
              >
                <SelectValue>{comparisonBase ?? automaticLabel}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={automaticBase}>{automaticLabel}</SelectItem>
                {branches.data?.branches.map(({ name, isCurrent }) =>
                  isCurrent || name === "HEAD" ? null : (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ),
                )}
                {branches.isPending && baseOpen ? (
                  <DiffLoading title={t("workspace.changesLoading")} />
                ) : null}
                {branches.isError ? (
                  <p
                    role="alert"
                    className="text-destructive type-caption px-2 py-1"
                  >
                    {t("workspace.changesLoadError")}
                  </p>
                ) : null}
              </SelectContent>
            </Select>
          ) : source === "history" ? (
            <Select
              value={commit}
              onValueChange={(value) => {
                setSelectedCommit(value);
                resetExpansion();
              }}
              disabled={!history.data?.length}
            >
              <SelectTrigger
                aria-label={t("workspace.git.commitHistory")}
                size="sm"
                className="max-w-56 min-w-0"
              >
                <SelectValue placeholder={t("workspace.git.chooseCommit")} />
              </SelectTrigger>
              <SelectContent>
                {history.data?.map(({ hash, hashFull, subject }) => (
                  <SelectItem key={hashFull} value={hashFull} title={subject}>
                    {hash} {subject}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          {!initialLoading && groups.length ? (
            <DiffLineCounts
              {...counts}
              unavailable={counts.unavailable || comparisonTruncated}
            />
          ) : null}
          <DiffControl
            label={t("workspace.git.refreshDiff")}
            onClick={refresh}
            disabled={pendingRefresh}
          >
            <RefreshCw />
          </DiffControl>
          <DiffControl
            label={t(
              anyExpanded
                ? "workspace.git.collapseAllDiffs"
                : "workspace.git.expandAllDiffs",
            )}
            onClick={() =>
              setExpansion({ all: !anyExpanded, exceptions: new Set() })
            }
            disabled={!hasFiles}
          >
            {anyExpanded ? <ChevronsDownUp /> : <ChevronsUpDown />}
          </DiffControl>
          <DiffControl
            label={t(
              diffMode === "split"
                ? "workspace.diffUnified"
                : "workspace.diffSplit",
            )}
            onClick={() =>
              setDiffMode(diffMode === "split" ? "unified" : "split")
            }
          >
            {diffMode === "split" ? <AlignJustify /> : <Columns2 />}
          </DiffControl>
          <DiffControl
            label={t("workspace.git.wrapDiff")}
            pressed={wrap}
            onClick={() => setWrap(!wrap)}
          >
            <WrapText />
          </DiffControl>
          <DiffControl
            label={t("workspace.git.hideWhitespace")}
            pressed={ignoreWhitespace}
            onClick={() => setIgnoreWhitespace(!ignoreWhitespace)}
          >
            <Pilcrow />
          </DiffControl>
          <DiffControl
            label={t("workspace.git.hideTestFiles")}
            pressed={hideTests}
            onClick={() => setHideTests(!hideTests)}
          >
            <FlaskConicalOff />
          </DiffControl>
          <DiffControl
            label={t(
              treeOpen ? "workspace.git.hideList" : "workspace.git.showList",
            )}
            pressed={treeOpen}
            onClick={() => setTreeOpen(!treeOpen)}
          >
            <Folders />
          </DiffControl>
        </div>
      </header>
      {comparisonTruncated ? (
        <p className="text-warning type-caption border-border-subtle border-b px-3 py-2">
          {t("workspace.git.comparisonTruncated")}
        </p>
      ) : null}
      {error || watchError || loadError ? (
        <p
          className="text-destructive type-caption border-border-subtle border-b px-3 py-2"
          role="alert"
        >
          {error ||
            watchError?.message ||
            (loadError instanceof Error
              ? loadError.message
              : t("workspace.changesLoadError"))}
        </p>
      ) : null}
      <WorkspaceDetailSplit
        sidebarOpen={treeOpen}
        resizeLabel={t("workbench.resizeSplit")}
        main={
          <main
            aria-label={t("workspace.git.diffPreview")}
            ref={main}
            className="min-h-0 min-w-0 flex-1 overflow-auto"
          >
            {initialLoading && !hasFiles ? (
              <DiffLoading title={t("workspace.changesLoading")} />
            ) : null}
            {!initialLoading && !hasFiles && !loadError ? (
              <GitState
                title={t(
                  source === "history" && !commit
                    ? history.data?.length
                      ? "workspace.git.chooseCommit"
                      : "workspace.git.historyEmpty"
                    : "workspace.git.diffEmpty",
                )}
              />
            ) : null}
            {groups
              .filter(({ files }) => files.length)
              .map((group) => (
                <ScopedGitFileList
                  key={group.id}
                  busy={busy}
                  group={group}
                  expanded={(key) =>
                    expansion.all !== expansion.exceptions.has(key)
                  }
                  focused={
                    focused ??
                    (activePath ? diffFileKey(group.id, activePath) : undefined)
                  }
                  onToggle={toggle}
                  onOpenFile={
                    onOpenFile ? (path) => onOpenFile(repo, path) : undefined
                  }
                  onStage={
                    source === "index"
                      ? (path) => {
                          const action =
                            group.id === ":staged"
                              ? unstageScopedGitFile
                              : stageScopedGitFile;
                          const observation = observeIndex(group.id);
                          void run(() =>
                            action(chatId, root, repo, path, observation),
                          );
                        }
                      : undefined
                  }
                  onDiscard={group.id === ":unstaged" ? discard : undefined}
                  renderPatch={({ path, lineCountUnavailable }) => (
                    <DeferredDiffBody
                      active={active}
                      chatId={chatId}
                      repository={repository}
                      base={group.id === automaticBase ? undefined : group.id}
                      path={path}
                      diffMode={diffMode}
                      wrap={wrap}
                      ignoreWhitespace={ignoreWhitespace}
                      lineCountUnavailable={lineCountUnavailable}
                    />
                  )}
                />
              ))}
          </main>
        }
        sidebar={
          treeOpen ? (
            <aside
              className="flex h-full min-h-0 flex-col"
              aria-label={t("workspace.git.changedFiles")}
            >
              <div className="shrink-0 p-2">
                <SearchField
                  width="full"
                  aria-label={t("workspace.searchChanges")}
                  placeholder={t("workspace.searchChangesPlaceholder")}
                  clearLabel={t("common:clearSearch")}
                  value={onQueryChange ? query : localQuery}
                  onChange={(event) =>
                    (onQueryChange ?? setLocalQuery)(event.currentTarget.value)
                  }
                />
              </div>
              <div className="min-h-0 flex-1 overflow-auto px-2 pb-2">
                <ScopedGitChangedTree
                  groups={groups.filter(({ files }) => files.length)}
                  focused={focused}
                  onJump={jump}
                />
              </div>
            </aside>
          ) : null
        }
      />
    </div>
  );
}

function isTestPath(path: string) {
  return /(?:^|\/)(?:__tests__|tests?|specs?)(?:\/|$)|(?:\.test\.|\.spec\.|_test\.|_spec\.|Tests?\.[^/]+$)|(?:^|\/)test_[^/]+\.[^/]+$/i.test(
    path,
  );
}

function repositoryName(path: string) {
  return path.split(/[\\/]/).at(-1) || path;
}

function DiffControl({
  label,
  pressed,
  disabled,
  onClick,
  children,
}: {
  label: string;
  pressed?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={label}
          aria-pressed={pressed}
          disabled={disabled}
          size="icon-sm"
          variant={pressed ? "secondary" : "ghost"}
          onClick={onClick}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function DeferredDiffBody({
  active,
  chatId,
  repository,
  base,
  path,
  diffMode,
  wrap,
  ignoreWhitespace,
  lineCountUnavailable,
}: {
  active: boolean;
  chatId?: string;
  repository: ChatGitRepository;
  base?: string;
  path: string;
  diffMode: DiffMode;
  wrap: boolean;
  ignoreWhitespace: boolean;
  lineCountUnavailable?: DiffEntry["lineCountUnavailable"];
}) {
  const { t } = useTranslation("chat");
  const content = useRef<HTMLDivElement>(null);
  const [eligible, setEligible] = useState(false);
  useEffect(() => {
    if (!active || !content.current) return;
    return observeViewportActivation(content.current, () => setEligible(true));
  }, [active]);
  const patch = useQuery({
    ...gitComparisonOptions(
      chatId,
      repository.root,
      repository.path,
      base,
      path,
      ignoreWhitespace,
    ),
    enabled: active && eligible,
    staleTime: 30_000,
  });
  return (
    <div ref={content} className="min-w-0">
      {patch.isPending ? (
        <DiffLoading title={t("workspace.changesLoading")} />
      ) : null}
      {patch.isError && !patch.data ? (
        <div
          role="alert"
          className="text-destructive type-caption flex items-center justify-between gap-2 px-3 py-2"
        >
          <span>{t("workspace.git.diffLoadError")}</span>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => void patch.refetch()}
          >
            {t("workspace.retry")}
          </Button>
        </div>
      ) : null}
      {patch.data?.truncated ? (
        <p className="text-warning type-caption px-3 py-2">
          {t("workspace.git.patchTruncated")}
        </p>
      ) : null}
      {patch.data?.content && /^@@ /m.test(patch.data.content) ? (
        <Suspense
          fallback={<DiffLoading title={t("workspace.changesLoading")} />}
        >
          <GitPatchBody
            path={path}
            patch={patch.data.content}
            mode={diffMode}
            wrap={wrap}
          />
        </Suspense>
      ) : patch.data && !patch.data.truncated ? (
        <p className="type-caption text-content-muted px-3 py-2">
          {t(
            lineCountUnavailable
              ? "workspace.git.unavailableLineCount"
              : "workspace.git.textDiffEmpty",
          )}
        </p>
      ) : null}
    </div>
  );
}

function DiffLoading({ title }: { title: string }) {
  return (
    <div
      role="status"
      aria-label={title}
      aria-busy="true"
      className="grid gap-2 px-3 py-2"
    >
      <Skeleton className="h-row-compact w-2/3" />
      <Skeleton className="h-row-compact w-1/2" />
    </div>
  );
}

function GitState({
  title,
  children,
}: {
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="text-muted-foreground type-body flex min-h-32 flex-col items-center justify-center gap-3 px-4 text-center">
      <FileDiff className="size-icon-xl" />
      <span>{title}</span>
      {children}
    </div>
  );
}
