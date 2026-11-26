import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  ChevronsDownUp,
  ChevronsUpDown,
  Columns2,
  Ellipsis,
  FileDiff,
  FlaskConicalOff,
  FolderTree,
  Pilcrow,
  RefreshCw,
  Rows3,
  WrapText,
} from "lucide-react";
import {
  lazy,
  Suspense,
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { cn } from "@/shared/lib/cn";
import {
  discardScopedGitFile,
  getScopedGitLog,
  stageScopedGitFile,
  unstageScopedGitFile,
  type ScopedGitStatus,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Skeleton } from "@/shared/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  gitComparisonOptions,
  gitStatusOptions,
  invalidateChatGit,
  useChatGitRepositories,
  type ChatGitRepository,
} from "../model/chat-git";
import { useDiffPatchViewport } from "./diff-patch-viewport";
import type { DiffMode } from "./git-patch-preview";
import { GitComparisonBasePicker } from "./scoped-git-base-picker";
import {
  diffFileKey,
  DiffLineCounts,
  ScopedGitChangedTree,
  ScopedGitFileList,
  type DiffEntry,
  type DiffGroup,
} from "./scoped-git-file-list";
import { useWorkspaceConfirmation } from "./use-workspace-confirmation";
import {
  fileTreeSidebarRatio,
  WorkspaceDetailSplit,
} from "./workspace-detail-split";
import { WorkspaceSurfaceHeader } from "./workspace-surface-header";

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
const diffTreeStorageKey = "bot.diff-file-tree-open";
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
  const [reviewed, setReviewed] = useState<{
    chatId?: string;
    repository: ChatGitRepository;
  }>();
  // 探测缓存回收后暂用原仓库身份，保留同一审查页直到 Core 返回新探测结果。
  const retained =
    reviewed &&
    reviewed.chatId === scopeChatId &&
    statuses[roots.indexOf(reviewed.repository.root)]?.isPending &&
    !repositories.some(({ path }) => path === reviewed.repository.path)
      ? reviewed.repository
      : undefined;
  const choices = retained ? [...repositories, retained] : repositories;
  const selected =
    choices.find(({ path }) => path === chosen) ??
    choices.find(({ path }) => path === preferredRepository) ??
    choices.find(({ root }) => root === preferredRoot) ??
    retained ??
    choices[0];
  if (
    active &&
    selected &&
    (!reviewed ||
      reviewed.chatId !== scopeChatId ||
      reviewed.repository.root !== selected.root ||
      reviewed.repository.path !== selected.path)
  )
    setReviewed({ chatId: scopeChatId, repository: selected });
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
      repositories={choices}
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
  const [selectedCommit, setSelectedCommit] = useState("");
  const [diffMode, setDiffMode] = useState<DiffMode>("unified");
  const [wrap, setWrap] = useState(false);
  const [ignoreWhitespace, setIgnoreWhitespace] = useState(false);
  const [hideTests, setHideTests] = useState(false);
  const [treeOpen, setTreeOpen] = useState(() => {
    try {
      return localStorage.getItem(diffTreeStorageKey) === "true";
    } catch {
      return false;
    }
  });
  const [localQuery, setLocalQuery] = useState("");
  const [expansion, setExpansion] = useState({
    all: false,
    exceptions: new Set<string>(),
  });
  const [focused, setFocused] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const main = useRef<HTMLElement>(null);
  const discardScope = useRef(0);
  useLayoutEffect(
    () => () => {
      discardScope.current++;
    },
    [active, chatId, repository.root, repository.path],
  );

  const { root, path: repo } = repository;
  const status = useQuery({
    ...gitStatusOptions(chatId, root, repo),
    enabled: active && source === "index" && !discoveredStatus,
  });
  const observedStatus = discoveredStatus ?? status.data;
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
  const branchComparison =
    source === "changes" ? comparisons[0]?.data : undefined;
  const comparisonHead = branchComparison?.branch ?? observedStatus?.branch;
  const displayedBase =
    branchComparison?.baseRef ?? comparisonBase ?? autoLabel;
  const multiRepository = repositories.length > 1;
  const toggleTree = () => {
    const next = !treeOpen;
    setTreeOpen(next);
    try {
      localStorage.setItem(diffTreeStorageKey, String(next));
    } catch {
      // 存储不可用时保留当前标签内的选择。
    }
  };

  const resetExpansion = () => {
    setExpansion({ all: false, exceptions: new Set() });
    setFocused(undefined);
  };
  const toggle = useCallback((key: string) => {
    setExpansion(({ all, exceptions }) => {
      const next = new Set(exceptions);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return { all, exceptions: next };
    });
  }, []);
  const openDiffFile = useCallback(
    (path: string) => onOpenFile?.(repo, path),
    [onOpenFile, repo],
  );
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
    const scope = discardScope.current;
    void run(async () => {
      const options = gitComparisonOptions(
        chatId,
        root,
        repo,
        ":unstaged",
        path,
        ignoreWhitespace,
        "",
      );
      // 保留已显示补丁的版本。收起的文件在确认前读取当前补丁。
      const displayed = queryClient.getQueryCache().find(options)?.isActive()
        ? queryClient.getQueryData(options.queryKey)
        : undefined;
      const { worktreeVersion } =
        displayed ??
        (await queryClient.fetchQuery({ ...options, staleTime: 0 }));
      if (scope !== discardScope.current) return false;
      if (!worktreeVersion) throw new Error(t("workspace.git.diffLoadError"));
      const accepted = await confirmation.confirm({ kind: "discard", path });
      return (
        accepted &&
        scope === discardScope.current &&
        discardScopedGitFile(chatId, root, repo, path, {
          expectedWorktreeVersion: worktreeVersion,
          ignoreWhitespace,
        })
      );
    });
  };

  const fileExpansionControl = {
    label: t(
      anyExpanded
        ? "workspace.git.collapseAllDiffs"
        : "workspace.git.expandAllDiffs",
    ),
    icon: anyExpanded ? <ChevronsDownUp /> : <ChevronsUpDown />,
    onClick: () => setExpansion({ all: !anyExpanded, exceptions: new Set() }),
  };
  const layoutOptions = [
    {
      value: "unified",
      label: t("workspace.diffUnified"),
      icon: <Rows3 />,
    },
    { value: "split", label: t("workspace.diffSplit"), icon: <Columns2 /> },
  ] as const;
  const displayControls = [
    {
      label: t("workspace.git.wrapDiff"),
      icon: <WrapText />,
      pressed: wrap,
      onClick: () => setWrap(!wrap),
    },
    {
      label: t("workspace.git.hideWhitespace"),
      icon: <Pilcrow />,
      pressed: ignoreWhitespace,
      onClick: () => setIgnoreWhitespace(!ignoreWhitespace),
    },
    {
      label: t("workspace.git.hideTestFiles"),
      icon: <FlaskConicalOff />,
      pressed: hideTests,
      onClick: () => setHideTests(!hideTests),
    },
  ];

  return (
    <div className="bg-surface-panel flex h-full min-h-0 min-w-0 flex-col">
      {confirmation.dialog}
      <WorkspaceSurfaceHeader className="@container/diff-header">
        <div className="@container/diff-refs flex min-w-0 flex-1 items-center gap-1 @max-[24.5rem]/diff-header:gap-0.5">
          <DiffToolbarMenu<DiffSource>
            label={t("workspace.git.diffSource")}
            value={source}
            options={[
              { value: "changes", label: t("workspace.git.sourceChanges") },
              {
                value: "uncommitted",
                label: t("workspace.git.sourceUncommitted"),
              },
              { value: "history", label: t("workspace.git.commitHistory") },
              { value: "index", label: t("workspace.git.sourceIndex") },
            ]}
            onChange={(value) => {
              setSource(value);
              resetExpansion();
            }}
          />
          {multiRepository && (
            <DiffToolbarMenu
              label={t("workspace.gitRepository")}
              value={repo}
              title={repo}
              options={repositories.map(({ path }) => ({
                value: path,
                title: path,
                label: repositories.some(
                  (other) =>
                    other.path !== path &&
                    repositoryName(other.path) === repositoryName(path),
                )
                  ? path
                  : repositoryName(path),
              }))}
              onChange={onRepositoryChange}
            />
          )}
          {source === "changes" ? (
            <>
              {comparisonHead && displayedBase && (
                <span
                  className="text-content-muted type-ui flex min-w-0 items-center gap-1.5 @max-[20rem]/diff-refs:hidden"
                  title={`${comparisonHead} → ${displayedBase}`}
                >
                  <span className="max-w-44 truncate">{comparisonHead}</span>
                  <ArrowRight
                    aria-hidden="true"
                    className="size-icon-sm shrink-0"
                  />
                </span>
              )}
              <GitComparisonBasePicker
                active={active}
                chatId={chatId}
                root={root}
                repository={repo}
                value={comparisonBase ?? ""}
                label={displayedBase ?? automaticLabel}
                automaticLabel={automaticLabel}
                title={
                  comparisonHead && displayedBase
                    ? `${comparisonHead} → ${displayedBase}`
                    : displayedBase
                }
                onChange={(base) => {
                  if (onComparisonBaseChange)
                    onComparisonBaseChange(repo, base);
                  else setLocalBase(base);
                  resetExpansion();
                }}
              />
            </>
          ) : source === "history" ? (
            <DiffToolbarMenu
              label={t("workspace.git.commitHistory")}
              value={commit}
              placeholder={t("workspace.git.chooseCommit")}
              options={(history.data ?? []).map(
                ({ hash, hashFull, subject }) => ({
                  value: hashFull,
                  label: `${hash} ${subject}`,
                  title: subject,
                }),
              )}
              onChange={(value) => {
                setSelectedCommit(value);
                resetExpansion();
              }}
            />
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {!initialLoading && groups.length ? (
            <span className="@max-[30rem]/diff-header:hidden">
              <DiffLineCounts
                {...counts}
                format="compact"
                unavailable={counts.unavailable || comparisonTruncated}
              />
            </span>
          ) : null}
          <DiffControl
            label={t("workspace.git.refreshDiff")}
            onClick={refresh}
            disabled={pendingRefresh}
          >
            <RefreshCw />
          </DiffControl>
          <div
            className={cn(
              "flex items-center gap-1",
              multiRepository
                ? "@max-[24.5rem]/diff-header:hidden"
                : "@max-[22rem]/diff-header:hidden",
            )}
          >
            <DiffControl
              label={fileExpansionControl.label}
              onClick={fileExpansionControl.onClick}
              disabled={!hasFiles}
            >
              {fileExpansionControl.icon}
            </DiffControl>
            <SegmentedControl
              appearance="compact-icons"
              ariaLabel={t("workspace.git.diffLayout")}
              options={layoutOptions}
              value={diffMode}
              onValueChange={setDiffMode}
            />
            {displayControls.map(({ icon, ...control }) => (
              <DiffControl key={control.label} {...control}>
                {icon}
              </DiffControl>
            ))}
          </div>
          <DropdownMenu modal={false}>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={t("workspace.git.diffViewOptions")}
                title={t("workspace.git.diffViewOptions")}
                size="icon-sm"
                variant="ghost"
                className={cn(
                  "hidden",
                  multiRepository
                    ? "@max-[24.5rem]/diff-header:inline-flex"
                    : "@max-[22rem]/diff-header:inline-flex",
                )}
              >
                <Ellipsis />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                disabled={!hasFiles}
                onSelect={fileExpansionControl.onClick}
              >
                {fileExpansionControl.icon}
                {fileExpansionControl.label}
              </DropdownMenuItem>
              {layoutOptions
                .filter(({ value }) => value !== diffMode)
                .map(({ label, value, icon }) => (
                  <DropdownMenuItem
                    key={value}
                    onSelect={() => setDiffMode(value)}
                  >
                    {icon}
                    {label}
                  </DropdownMenuItem>
                ))}
              {displayControls.map(({ label, icon, pressed, onClick }) => (
                <DropdownMenuCheckboxItem
                  key={label}
                  checked={pressed}
                  onCheckedChange={onClick}
                >
                  {icon}
                  {label}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <DiffControl
            label={t(
              treeOpen ? "workspace.git.hideList" : "workspace.git.showList",
            )}
            pressed={treeOpen}
            onClick={toggleTree}
          >
            <FolderTree />
          </DiffControl>
        </div>
      </WorkspaceSurfaceHeader>
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
        defaultSidebarRatio={fileTreeSidebarRatio}
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
                  onOpenFile={onOpenFile ? openDiffFile : undefined}
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
                  renderPatch={({ path, oldPath, lineCountUnavailable }) => (
                    <DeferredDiffBody
                      active={active}
                      chatId={chatId}
                      repository={repository}
                      base={group.id === automaticBase ? undefined : group.id}
                      path={path}
                      previousPath={oldPath ?? ""}
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
              className="bg-surface-panel flex h-full min-h-0 min-w-0 flex-col"
              aria-label={t("workspace.git.changedFiles")}
            >
              <div className="shrink-0 p-2">
                <SearchField
                  width="full"
                  size="sm"
                  aria-label={t("workspace.searchChanges")}
                  placeholder={t("workspace.searchChangesPlaceholder")}
                  clearLabel={t("common:clearSearch")}
                  value={onQueryChange ? query : localQuery}
                  onChange={(event) =>
                    (onQueryChange ?? setLocalQuery)(event.currentTarget.value)
                  }
                />
              </div>
              <div className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2">
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

function DiffToolbarMenu<Value extends string>({
  label,
  value,
  options,
  title,
  placeholder,
  onChange,
}: {
  label: string;
  value: Value;
  options: ReadonlyArray<{ value: Value; label: string; title?: string }>;
  title?: string;
  placeholder?: string;
  onChange: (value: Value) => void;
}) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <FieldControlTrigger
          aria-label={label}
          appearance="toolbar"
          width={null}
          size="sm"
          className="max-w-44 min-w-0"
          title={title}
          disabled={!options.length}
        >
          <span>
            {options.find((item) => item.value === value)?.label ?? placeholder}
          </span>
        </FieldControlTrigger>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuRadioGroup value={value}>
          {options.map((option) => (
            <DropdownMenuRadioItem
              key={option.value}
              value={option.value}
              title={option.title}
              onSelect={() => {
                if (option.value !== value) onChange(option.value);
              }}
            >
              <span className="min-w-0 truncate">{option.label}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
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
  previousPath,
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
  previousPath: string;
  diffMode: DiffMode;
  wrap: boolean;
  ignoreWhitespace: boolean;
  lineCountUnavailable?: DiffEntry["lineCountUnavailable"];
}) {
  const { t } = useTranslation("chat");
  const content = useRef<HTMLDivElement>(null);
  const inViewport = useDiffPatchViewport(content, active);
  const patch = useQuery({
    ...gitComparisonOptions(
      chatId,
      repository.root,
      repository.path,
      base,
      path,
      ignoreWhitespace,
      previousPath,
    ),
    enabled: inViewport,
    staleTime: 30_000,
    gcTime: 60_000,
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
