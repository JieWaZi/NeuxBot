import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, GitCommitHorizontal, GitPullRequest } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
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
import { invalidateChatGit } from "../model/chat-git";

type Action = Extract<
  WorkspaceGitAction["action"],
  | "commit"
  | "push"
  | "push_pr"
  | "commit_push"
  | "create_pr"
  | "commit_push_pr"
  | "find_pr"
>;

const actions: readonly Action[] = [
  "commit",
  "commit_push",
  "push",
  "push_pr",
  "commit_push_pr",
  "create_pr",
  "find_pr",
];

const includesCommit = (action: Action) =>
  action === "commit" ||
  action === "commit_push" ||
  action === "commit_push_pr";

const includesPullRequest = (action: Action) =>
  action === "create_pr" ||
  action === "commit_push_pr" ||
  action === "push_pr" ||
  action === "find_pr";

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
  const remotes = status.remotes ?? (status.remote ? [status.remote] : []);
  const canSelectRemote = Boolean(
    status.branch && status.branch !== "HEAD (no branch)" && remotes.length,
  );
  const canPublish = canSelectRemote && (!status.behind || remotes.length > 1);
  const canUsePR = Boolean(status.github || remotes.length > 1);
  const canCommit = committableFiles(status).length > 0;
  const run = useMutation({
    mutationFn: (input: WorkspaceGitAction) => runWorkspaceGitAction(input),
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
  const selectionCount = selectedFiles?.size ?? staged.length;
  const missingFiles = Boolean(
    pendingAction && includesCommit(pendingAction) && selectionCount === 0,
  );
  const toggleFile = (path: string, checked: boolean) => {
    setSelectedFiles((current) => {
      if (!current) return current;
      const next = new Set(current);
      if (checked) next.add(path);
      else next.delete(path);
      return next;
    });
  };
  const remoteRequired =
    pendingAction !== undefined && pendingAction !== "commit";
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
        pendingAction !== "find_pr" &&
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
    if (!confirmation) return;
    const { action, status: observed } = confirmation;
    run.mutate({
      ...target,
      action,
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
          <GitCommitHorizontal />
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
            {actions.map((action) => (
              <DropdownMenuItem
                key={action}
                onSelect={() => begin(action)}
                disabled={
                  (action !== "commit" &&
                    !(action === "find_pr" || action === "create_pr"
                      ? canSelectRemote
                      : canPublish)) ||
                  (includesPullRequest(action) && !canUsePR) ||
                  (includesCommit(action) && !canCommit)
                }
              >
                {t(`summary.git.actions.${action}`)}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {result?.commit ||
      result?.pushed ||
      result?.prUrl ||
      failure ||
      (result && run.variables?.action === "find_pr") ? (
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
          ) : result && !failure && run.variables?.action === "find_pr" ? (
            <span>{t("summary.git.noPR")}</span>
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
          title={pendingAction ? t(`summary.git.actions.${pendingAction}`) : ""}
          size="sm"
          onEscapeKeyDown={(event) => {
            if (run.isPending) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (run.isPending) event.preventDefault();
          }}
        >
          <div className="grid min-h-0 gap-3 overflow-y-auto px-5 pb-4">
            <p className="type-caption text-content-muted wrap-anywhere">
              {target.repository} · {confirmation?.status.branch || "HEAD"}
              {remote && confirmedRemoteBranch
                ? ` → ${remote}/${confirmedRemoteBranch}`
                : null}
            </p>
            {pendingAction && includesCommit(pendingAction) ? (
              <>
                <div className="flex items-center justify-between gap-2">
                  {hasSelection ? (
                    <label className="type-ui flex items-center gap-2">
                      <Checkbox
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
                      {t("summary.git.selectAllFiles")}
                    </label>
                  ) : (
                    <span className="type-ui">{t("workspace.git.staged")}</span>
                  )}
                  {staged.length > 0 ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={run.isPending}
                      onClick={() =>
                        setSelectedFiles(
                          hasSelection
                            ? undefined
                            : new Set(staged.map(({ path }) => path)),
                        )
                      }
                    >
                      {t(
                        hasSelection
                          ? "summary.git.useStaged"
                          : "summary.git.selectFiles",
                      )}
                    </Button>
                  ) : null}
                </div>
                <ul className="type-caption text-content-muted grid max-h-36 gap-2 overflow-y-auto">
                  {(hasSelection ? files : staged).map(({ path }) => (
                    <li className="wrap-anywhere" key={path}>
                      {hasSelection ? (
                        <label className="flex items-center gap-2">
                          <Checkbox
                            size="sm"
                            checked={selectedFiles.has(path)}
                            disabled={run.isPending}
                            onCheckedChange={(checked) =>
                              toggleFile(path, checked === true)
                            }
                          />
                          {path}
                        </label>
                      ) : (
                        path
                      )}
                    </li>
                  ))}
                </ul>
                {hasSelection && staged.length ? (
                  <p className="type-caption text-content-muted">
                    {t("summary.git.commitWorkingContents")}
                  </p>
                ) : null}
                <Textarea
                  aria-label={t("workspace.git.commitMessage")}
                  placeholder={t("summary.git.autoMessage")}
                  maxLength={4096}
                  disabled={run.isPending}
                  value={message}
                  onChange={(event) => setMessage(event.target.value)}
                />
              </>
            ) : null}
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
            {pendingAction && includesPullRequest(pendingAction) ? (
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
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={run.isPending}
              onClick={() => setConfirmation(undefined)}
            >
              {t("common:cancel")}
            </Button>
            <Button
              disabled={
                run.isPending || disabled || unsuitableRemote || missingFiles
              }
              onClick={submit}
            >
              {run.isPending
                ? t("summary.git.running")
                : pendingAction
                  ? t(`summary.git.actions.${pendingAction}`)
                  : ""}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
