import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUp,
  ChevronDown,
  GitBranch,
  GitCommitHorizontal,
  GitPullRequest,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  getScopedGitStatus,
  openExternalURL,
  runWorkspaceGitAction,
  type ScopedGitStatus,
  type WorkspaceGitAction,
  type WorkspaceGitTarget,
} from "@/shared/platform";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Input } from "@/shared/ui/input";
import { OverflowText } from "@/shared/ui/overflow-text";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Textarea } from "@/shared/ui/textarea";
import { gitComparisonOptions, invalidateChatGit } from "../model/chat-git";
import { DiffLineCounts } from "./scoped-git-file-list";

type Action = Extract<
  WorkspaceGitAction["action"],
  "commit" | "push" | "push_pr" | "commit_push" | "create_pr" | "commit_push_pr"
>;

type ActionInput = WorkspaceGitAction & { newBranch?: string };

const includesCommit = (action: Action) =>
  action === "commit" ||
  action === "commit_push" ||
  action === "commit_push_pr";

const includesPullRequest = (action: Action) =>
  action === "create_pr" || action === "commit_push_pr" || action === "push_pr";

const unmergedStatuses = new Set(["DD", "AU", "UD", "UA", "DU", "AA", "UU"]);

function committableFiles(status: ScopedGitStatus) {
  return status.entries.filter(({ xy }) => !unmergedStatuses.has(xy));
}

function stagedFiles(files: ScopedGitStatus["entries"]) {
  return files.filter(({ xy }) => /^[MADRCT]/.test(xy));
}

function destinationBranch(status: ScopedGitStatus, remote: string) {
  const prefix = `${remote}/`;
  return status.upstream?.startsWith(prefix)
    ? status.upstream.slice(prefix.length)
    : status.branch;
}

export function ChatGitActions({
  target,
  status,
  disabled,
}: {
  target: WorkspaceGitTarget & { repository: string };
  status: ScopedGitStatus;
  disabled: boolean;
}) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [confirmation, setConfirmation] = useState<{
    action: Action;
    status: ScopedGitStatus;
    expectedRemoteBranch?: string;
  }>();
  const [message, setMessage] = useState("");
  const [base, setBase] = useState("");
  const [remote, setRemote] = useState("");
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>();
  const [isEditingFiles, setEditingFiles] = useState(false);
  const [newBranchName, setNewBranchName] = useState<string>();
  const [createdBranch, setCreatedBranch] = useState(false);
  const remotes = status.remotes ?? (status.remote ? [status.remote] : []);
  const canSelectRemote = Boolean(
    status.branch && status.branch !== "HEAD (no branch)" && remotes.length,
  );
  const canPublish = canSelectRemote && (!status.behind || remotes.length > 1);
  const canUsePR = Boolean(status.github || remotes.length > 1);
  const canCommit = committableFiles(status).length > 0;
  const run = useMutation({
    mutationFn: async ({ newBranch, ...input }: ActionInput) => {
      if (!newBranch) return runWorkspaceGitAction(input);
      const { chatId, root, repository } = input;
      const branchResult = await runWorkspaceGitAction({
        chatId,
        root,
        repository,
        action: "create_branch",
        branch: newBranch,
        expectedBranch: input.expectedBranch,
        expectedHead: input.expectedHead,
      });
      if (branchResult.failedStep || branchResult.branch !== newBranch) {
        throw new Error(
          applicationErrorMessage({
            code: branchResult.error,
            message: branchResult.errorMessage,
            messageSource: branchResult.errorMessage ? "public" : undefined,
            fallback: t("summary.git.actionFailed"),
          }),
        );
      }
      setCreatedBranch(true);
      setNewBranchName(undefined);
      setConfirmation((current) =>
        current
          ? {
              action: "commit",
              status: {
                ...current.status,
                branch: newBranch,
                upstream: undefined,
              },
            }
          : current,
      );
      const current = await getScopedGitStatus(chatId, root, repository);
      if (
        current.branch !== newBranch ||
        (current.head ?? "") !== input.expectedHead ||
        (current.index ?? "") !== (input.expectedIndex ?? "")
      ) {
        throw new Error(t("summary.git.retryStale"));
      }
      setConfirmation((checkpoint) =>
        checkpoint
          ? {
              ...checkpoint,
              status: { ...checkpoint.status, upstream: current.upstream },
            }
          : checkpoint,
      );
      return runWorkspaceGitAction({
        ...input,
        expectedBranch: newBranch,
        expectedUpstream: current.upstream,
      });
    },
    onSuccess: (result) => {
      if (!result.failedStep || result.commit || result.pushed)
        setConfirmation(undefined);
    },
    onSettled: () =>
      invalidateChatGit(queryClient, target.root, target.repository),
  });
  const knownPR =
    run.data?.prUrl &&
    run.data.branch === status.branch &&
    run.variables?.remote === status.remote;
  const proposesPR = Boolean(
    status.github &&
    status.defaultBranch &&
    status.branch !== status.defaultBranch &&
    !knownPR,
  );
  const aheadOfUpstream =
    status.ahead || (!status.upstream && status.aheadOfDefault);
  let quickAction: Action = "commit";
  if (canPublish && status.remote && !status.behind) {
    if (canCommit) {
      quickAction = proposesPR ? "commit_push_pr" : "commit_push";
    } else if (aheadOfUpstream) {
      quickAction = proposesPR ? "push_pr" : "push";
    } else if (proposesPR && status.aheadOfDefault) {
      quickAction = "create_pr";
    }
  }
  const begin = (
    action: Action,
    destination?: Pick<
      WorkspaceGitAction,
      "remote" | "base" | "expectedRemoteBranch"
    >,
    expected?: Pick<ScopedGitStatus, "branch" | "head">,
  ) => {
    run.reset();
    setConfirmation({
      action,
      status: expected ? { ...status, ...expected } : status,
      expectedRemoteBranch: destination?.expectedRemoteBranch,
    });
    setMessage("");
    setEditingFiles(false);
    setNewBranchName(undefined);
    setCreatedBranch(false);
    const files = committableFiles(status);
    setSelectedFiles(
      includesCommit(action) && !stagedFiles(files).length
        ? new Set(files.map(({ path }) => path))
        : undefined,
    );
    setRemote(destination?.remote ?? status.remote ?? "");
    setBase(destination?.base ?? "");
  };
  const result = run.data;
  const pendingAction = confirmation?.action;
  const files =
    confirmation && includesCommit(confirmation.action)
      ? committableFiles(confirmation.status)
      : [];
  const staged = stagedFiles(files);
  const hasSelection = selectedFiles !== undefined;
  const included = selectedFiles ?? new Set(staged.map(({ path }) => path));
  const selectionCount = included.size;
  const commitDialog =
    pendingAction !== undefined && includesCommit(pendingAction);
  const missingFiles = commitDialog && selectionCount === 0;
  const namingBranch = newBranchName !== undefined;
  const worktreeComparison = useQuery({
    ...gitComparisonOptions(
      target.chatId,
      target.root,
      target.repository,
      "HEAD",
    ),
    enabled: commitDialog && hasSelection,
  });
  const netStats = new Map(
    worktreeComparison.data?.branch === confirmation?.status.branch
      ? worktreeComparison.data?.entries.map((entry) => [entry.path, entry])
      : undefined,
  );
  const summaryFiles = (
    isEditingFiles ? files : files.filter(({ path }) => included.has(path))
  ).map((entry) => {
    const net = netStats.get(entry.path);
    const matchesIndex =
      !hasSelection || (entry.xy[1] === " " && entry.xy !== "??");
    const mixed = entry.xy[0] !== " " && entry.xy[1] !== " ";
    return {
      path: entry.path,
      added: matchesIndex
        ? entry.stagedAddedLines
        : (net?.addedLines ?? (mixed ? 0 : entry.unstagedAddedLines)),
      deleted: matchesIndex
        ? entry.stagedDeletedLines
        : (net?.deletedLines ?? (mixed ? 0 : entry.unstagedDeletedLines)),
      unavailable: matchesIndex
        ? entry.lineCountUnavailable
        : (net?.lineCountUnavailable ??
          (net ? false : mixed || entry.lineCountUnavailable)),
    };
  });
  const totals = summaryFiles.reduce(
    (sum, file) =>
      included.has(file.path)
        ? {
            added: sum.added + file.added,
            deleted: sum.deleted + file.deleted,
            unavailable: sum.unavailable || Boolean(file.unavailable),
          }
        : sum,
    { added: 0, deleted: 0, unavailable: false },
  );
  const toggleFile = (path: string, checked: boolean) => {
    const next = new Set(included);
    if (checked) next.add(path);
    else next.delete(path);
    setSelectedFiles(next);
  };
  const remoteRequired =
    !namingBranch && pendingAction !== undefined && pendingAction !== "commit";
  const confirmedRemoteBranch =
    remoteRequired && confirmation && remote
      ? (confirmation.expectedRemoteBranch ??
        destinationBranch(confirmation.status, remote))
      : undefined;
  const unsuitableRemote =
    remoteRequired &&
    (!remote ||
      (remote === status.remote &&
        Boolean(status.behind) &&
        pendingAction !== "create_pr"));
  const failure = result?.failedStep;
  const failureMessage = applicationErrorMessage({
    code: result?.error,
    message: result?.errorMessage,
    messageSource: result?.errorMessage ? "public" : undefined,
    fallback: t("summary.git.actionFailed"),
  });
  const retry =
    failure === "pr"
      ? "create_pr"
      : failure === "push"
        ? run.variables?.action === "commit_push_pr" ||
          run.variables?.action === "push_pr"
          ? "push_pr"
          : "push"
        : undefined;
  const checkpoint = {
    branch: result?.branch ?? run.variables?.expectedBranch,
    head: result?.commit ?? run.variables?.expectedHead,
  };
  const retryStale =
    !run.isPending &&
    ((status.branch ?? "") !== (checkpoint.branch ?? "") ||
      (status.head ?? "") !== (checkpoint.head ?? "") ||
      (run.variables?.expectedRemoteBranch &&
        run.variables.remote &&
        destinationBranch(status, run.variables.remote) !==
          run.variables.expectedRemoteBranch));
  const submit = () => {
    if (!confirmation || run.isPending) return;
    const { status: observed } = confirmation;
    const newBranch = newBranchName?.trim();
    if (namingBranch && !newBranch) return;
    const action = newBranch ? "commit" : confirmation.action;
    run.mutate({
      ...target,
      action,
      newBranch,
      message: includesCommit(action) ? message.trim() : undefined,
      filePaths: selectedFiles
        ? files
            .filter(({ path }) => selectedFiles.has(path))
            .map(({ path }) => path)
        : undefined,
      expectedHead: observed.head ?? "",
      expectedBranch: observed.branch ?? "",
      expectedIndex: includesCommit(action) ? observed.index : undefined,
      expectedUpstream: observed.upstream,
      expectedRemoteBranch: confirmedRemoteBranch,
      remote: action !== "commit" ? remote.trim() : undefined,
      base: includesPullRequest(action) ? base.trim() : undefined,
    });
  };

  const actionLabel = pendingAction
    ? t(`summary.git.actions.${pendingAction}`)
    : "";
  const currentBranch = confirmation?.status.branch || "HEAD";
  const FileRow = isEditingFiles ? "label" : "div";
  const canCreatePR =
    canPublish &&
    canUsePR &&
    Boolean(status.aheadOfDefault ?? status.ahead) &&
    !knownPR;
  const QuickIcon = !includesCommit(quickAction)
    ? includesPullRequest(quickAction)
      ? GitPullRequest
      : ArrowUp
    : GitCommitHorizontal;

  return (
    <>
      <div className="flex min-w-0 items-center">
        <Button
          size="row"
          variant="ghost"
          className="min-w-0 flex-1"
          disabled={
            disabled ||
            run.isPending ||
            (quickAction === "commit" && !canCommit)
          }
          onClick={() => begin(quickAction)}
        >
          <QuickIcon />
          <OverflowText className="min-w-0 text-left">
            {t(`summary.git.actions.${quickAction}`)}
          </OverflowText>
        </Button>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={disabled || run.isPending}
              aria-label={t("summary.git.options")}
              title={t("summary.git.options")}
            >
              <ChevronDown />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onSelect={() => begin("commit")}
              disabled={!canCommit}
            >
              <GitCommitHorizontal />
              {t("summary.git.actions.commit")}
            </DropdownMenuItem>
            {canSelectRemote ? (
              <>
                <DropdownMenuItem
                  onSelect={() => begin("push")}
                  disabled={!canPublish || !status.head}
                >
                  <ArrowUp />
                  {t("summary.git.actions.push")}
                </DropdownMenuItem>
                {canUsePR && !knownPR ? (
                  <DropdownMenuItem
                    onSelect={() =>
                      begin(aheadOfUpstream ? "push_pr" : "create_pr")
                    }
                    disabled={!canCreatePR}
                  >
                    <GitPullRequest />
                    {t("summary.git.createPR")}
                  </DropdownMenuItem>
                ) : null}
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {result?.commit || result?.pushed || result?.prUrl || failure ? (
        <div
          className="type-caption text-content-muted grid gap-1 px-2 pb-1"
          role={failure ? "alert" : "status"}
        >
          {result?.commit ? (
            <span>
              {t("summary.git.committed", {
                commit: result.commit.slice(0, 8),
              })}
            </span>
          ) : null}
          {result?.pushed ? <span>{t("summary.git.pushed")}</span> : null}
          {failure ? (
            <span>
              {t("summary.git.failedStep", {
                step: t(`summary.git.steps.${failure}`),
              })}{" "}
              · {failureMessage}
            </span>
          ) : null}
          {retry && retryStale ? (
            <span>{t("summary.git.retryStale")}</span>
          ) : retry && !disabled ? (
            <Button
              size="sm"
              variant="secondary"
              disabled={run.isPending}
              onClick={() =>
                begin(
                  retry,
                  {
                    remote: run.variables?.remote,
                    base: result?.base ?? run.variables?.base,
                    expectedRemoteBranch: run.variables?.expectedRemoteBranch,
                  },
                  checkpoint,
                )
              }
            >
              {t(`summary.git.actions.${retry}`)}
            </Button>
          ) : null}
          {result?.prUrl ? (
            <Button
              size="sm"
              variant="ghost"
              title={result.prUrl}
              onClick={() => {
                if (result.prUrl) void openExternalURL(result.prUrl);
              }}
            >
              <GitPullRequest />
              {t("summary.git.openPR")}
            </Button>
          ) : null}
        </div>
      ) : null}
      <Dialog
        open={Boolean(confirmation)}
        onOpenChange={(open) => {
          if (!open && !run.isPending) setConfirmation(undefined);
        }}
      >
        <DialogContent
          title={namingBranch ? t("summary.git.commitNewBranch") : actionLabel}
          aria-describedby={undefined}
          size={commitDialog && !namingBranch ? "md" : "sm"}
          onEscapeKeyDown={(event) => {
            if (run.isPending) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (run.isPending) event.preventDefault();
          }}
        >
          <div className="grid min-h-0 gap-3 overflow-y-auto px-6 pb-3">
            {commitDialog && !namingBranch ? (
              <>
                <div className="bg-surface-subtle grid min-w-0 gap-2 rounded-lg p-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <GitBranch
                      aria-hidden="true"
                      className="text-content-muted size-icon-sm shrink-0"
                    />
                    <OverflowText className="type-ui min-w-0 font-medium">
                      {currentBranch}
                    </OverflowText>
                    <OverflowText className="type-caption text-content-muted ml-auto min-w-0">
                      {target.repository}
                    </OverflowText>
                  </div>
                  {confirmedRemoteBranch ? (
                    <OverflowText className="type-caption text-content-muted min-w-0">
                      {`${remote}/${confirmedRemoteBranch}`}
                    </OverflowText>
                  ) : null}
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <div className="type-ui flex min-w-0 items-center gap-2">
                      {isEditingFiles ? (
                        <Checkbox
                          aria-label={t("summary.git.selectAllFiles")}
                          size="sm"
                          disabled={run.isPending}
                          checked={
                            selectionCount === files.length ||
                            (selectionCount > 0 ? "indeterminate" : false)
                          }
                          onCheckedChange={(checked) =>
                            setSelectedFiles(
                              new Set(
                                checked === true
                                  ? files.map(({ path }) => path)
                                  : [],
                              ),
                            )
                          }
                        />
                      ) : null}
                      <span>{t("workspace.files")}</span>
                      {selectionCount !== files.length ? (
                        <span className="type-caption text-content-muted tabular-nums">
                          {t("summary.git.selectedFiles", {
                            count: selectionCount,
                            total: files.length,
                          })}
                        </span>
                      ) : null}
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      {isEditingFiles && hasSelection && staged.length ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={run.isPending}
                          onClick={() => setSelectedFiles(undefined)}
                        >
                          {t("summary.git.useStaged")}
                        </Button>
                      ) : null}
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={run.isPending}
                        onClick={() => setEditingFiles(!isEditingFiles)}
                      >
                        {t(
                          isEditingFiles
                            ? "summary.git.done"
                            : "summary.git.edit",
                        )}
                      </Button>
                    </div>
                  </div>
                  <ul className="bg-surface-raised type-ui grid max-h-44 min-w-0 gap-1 overflow-y-auto rounded-md p-2">
                    {summaryFiles.map((file) => (
                      <li className="min-w-0" key={file.path}>
                        <FileRow className="flex min-w-0 items-center gap-2 px-1 py-1">
                          {isEditingFiles ? (
                            <Checkbox
                              aria-label={file.path}
                              size="sm"
                              checked={included.has(file.path)}
                              disabled={run.isPending}
                              onCheckedChange={(checked) =>
                                toggleFile(file.path, checked === true)
                              }
                            />
                          ) : null}
                          <OverflowText className="min-w-0 flex-1 font-mono">
                            {file.path}
                          </OverflowText>
                          {included.has(file.path) ? (
                            <DiffLineCounts {...file} format="grouped" />
                          ) : null}
                        </FileRow>
                      </li>
                    ))}
                  </ul>
                  <div className="flex justify-end">
                    <DiffLineCounts {...totals} format="grouped" />
                  </div>
                </div>
                {isEditingFiles && hasSelection && staged.length ? (
                  <p className="type-caption text-content-muted">
                    {t("summary.git.commitWorkingContents")}
                  </p>
                ) : null}
                <label className="type-ui grid gap-1.5 font-medium">
                  {t("summary.git.optionalMessage")}
                  <Textarea
                    size="compact"
                    placeholder={t("summary.git.autoMessage")}
                    maxLength={4096}
                    disabled={run.isPending}
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                  />
                </label>
              </>
            ) : namingBranch ? (
              <label className="type-ui grid gap-1.5">
                {t("summary.git.newBranchName")}
                <Input
                  autoFocus
                  size="sm"
                  value={newBranchName}
                  disabled={run.isPending}
                  onChange={(event) => setNewBranchName(event.target.value)}
                  onKeyDown={(event) => {
                    if (
                      event.key === "Enter" &&
                      !event.nativeEvent.isComposing &&
                      !disabled &&
                      !missingFiles
                    ) {
                      event.preventDefault();
                      submit();
                    }
                  }}
                />
              </label>
            ) : (
              <p className="type-caption text-content-muted wrap-anywhere">
                {target.repository} · {currentBranch}
                {remote && confirmedRemoteBranch
                  ? ` → ${remote}/${confirmedRemoteBranch}`
                  : null}
              </p>
            )}
            {remoteRequired ? (
              <Select
                value={remote}
                disabled={
                  run.isPending || Boolean(confirmation?.expectedRemoteBranch)
                }
                onValueChange={setRemote}
              >
                <SelectTrigger
                  aria-label={t("summary.git.remote")}
                  width="full"
                >
                  <SelectValue placeholder={t("summary.git.remote")} />
                </SelectTrigger>
                <SelectContent>
                  {remotes.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
            {!namingBranch &&
            pendingAction &&
            includesPullRequest(pendingAction) ? (
              <label className="type-ui grid gap-1">
                {t("summary.git.base")}
                <Input
                  placeholder={t("summary.git.defaultBase")}
                  value={base}
                  disabled={
                    run.isPending || Boolean(confirmation?.expectedRemoteBranch)
                  }
                  onChange={(event) => setBase(event.target.value)}
                />
              </label>
            ) : null}
            {run.isError || (failure && !result?.commit) ? (
              <p className="type-caption text-destructive" role="alert">
                {run.error?.message ?? failureMessage}
              </p>
            ) : null}
          </div>
          <DialogFooter variant="compact">
            <Button
              size="sm"
              variant="secondary"
              disabled={run.isPending}
              onClick={() =>
                namingBranch
                  ? setNewBranchName(undefined)
                  : setConfirmation(undefined)
              }
            >
              {namingBranch ? t("summary.git.back") : t("common:cancel")}
            </Button>
            {commitDialog && !namingBranch && !createdBranch ? (
              <Button
                size="sm"
                variant="secondary"
                disabled={run.isPending || disabled || missingFiles}
                onClick={() => setNewBranchName("")}
              >
                {t("summary.git.commitNewBranch")}
              </Button>
            ) : null}
            <Button
              size="sm"
              disabled={
                run.isPending ||
                disabled ||
                unsuitableRemote ||
                missingFiles ||
                (namingBranch && !newBranchName.trim())
              }
              onClick={submit}
            >
              {run.isPending
                ? t("summary.git.running")
                : namingBranch
                  ? t("summary.git.createBranchAndCommit")
                  : actionLabel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
