import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, GitBranch } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/sonner";

import type { WorkspaceGitCheckoutConflict } from "@/shared/platform";
import {
  checkoutWorkspaceGitBranch,
  hasPlatformService,
  listWorkspaceGitBranches,
  previewWorkspaceGitCheckout,
} from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import { OverflowText } from "@/shared/ui/overflow-text";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { Spinner } from "@/shared/ui/spinner";
import { Textarea } from "@/shared/ui/textarea";

type ChatComposerBranchSelectorProps = {
  className?: string;
  currentBranch: string;
  gitRoot: string;
  /** 多仓库并存时展示目录标签。 */
  repoLabel?: string;
  showRepoLabel?: boolean;
};

export function ChatComposerBranchSelector({
  className,
  currentBranch,
  gitRoot,
  repoLabel,
  showRepoLabel = false,
}: ChatComposerBranchSelectorProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [pendingBranch, setPendingBranch] = useState<string | null>(null);
  const [conflicts, setConflicts] = useState<
    WorkspaceGitCheckoutConflict[] | null
  >(null);
  const [commitMessage, setCommitMessage] = useState("");
  const [commitStep, setCommitStep] = useState(false);

  const branchesQuery = useQuery({
    queryKey: ["workspace-git-branches", gitRoot],
    queryFn: () => listWorkspaceGitBranches(gitRoot),
    enabled: open && Boolean(gitRoot) && hasPlatformService("git"),
    staleTime: 3000,
  });

  const filteredBranches = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    const branches = branchesQuery.data?.branches ?? [];
    if (!normalized) return branches;
    return branches.filter((branch) =>
      branch.name.toLocaleLowerCase().includes(normalized),
    );
  }, [branchesQuery.data?.branches, query]);

  const invalidateGitQueries = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["workspace-git-changes"] }),
      queryClient.invalidateQueries({ queryKey: ["workspace-git-branches"] }),
      queryClient.invalidateQueries({
        queryKey: ["workspace-git-repositories"],
      }),
    ]);
  };

  const checkoutMutation = useMutation({
    mutationFn: ({
      branch,
      commitMessage: message,
    }: {
      branch: string;
      commitMessage?: string;
    }) => checkoutWorkspaceGitBranch(gitRoot, branch, message),
    onSuccess: async () => {
      await invalidateGitQueries();
      setOpen(false);
      setPendingBranch(null);
      setConflicts(null);
      setCommitStep(false);
      setCommitMessage("");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t("branchSwitch.checkoutFailed"),
      );
    },
  });

  const resetConflictDialog = () => {
    setPendingBranch(null);
    setConflicts(null);
    setCommitStep(false);
    setCommitMessage("");
  };

  const handleSelectBranch = async (branch: string) => {
    if (branch === currentBranch) {
      setOpen(false);
      return;
    }
    try {
      const preview = await previewWorkspaceGitCheckout(gitRoot, branch);
      if (preview.canCheckout) {
        await checkoutMutation.mutateAsync({ branch });
        return;
      }
      setPendingBranch(branch);
      setConflicts(preview.conflicts);
      setCommitStep(false);
      setCommitMessage("");
      setOpen(false);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t("branchSwitch.previewFailed"),
      );
    }
  };

  const handleCommitAndSwitch = async () => {
    if (!pendingBranch) return;
    const message = commitMessage.trim();
    if (!message) {
      toast.error(t("branchSwitch.commitMessageRequired"));
      return;
    }
    await checkoutMutation.mutateAsync({
      branch: pendingBranch,
      commitMessage: message,
    });
  };

  const triggerLabel =
    showRepoLabel && repoLabel
      ? `${repoLabel} · ${currentBranch}`
      : currentBranch;

  if (!hasPlatformService("git")) {
    return (
      <span
        data-chat-composer-branch
        className={cn(
          "inline-flex h-[18px] max-w-28 min-w-0 items-center gap-1 px-1.5",
          className,
        )}
      >
        <GitBranch aria-hidden className="size-icon-xs shrink-0" />
        <OverflowText className="min-w-0 font-normal">
          {triggerLabel}
        </OverflowText>
      </span>
    );
  }

  return (
    <>
      <Popover
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen);
          if (!nextOpen) setQuery("");
        }}
      >
        <PopoverTrigger asChild>
          <Button
            type="button"
            data-testid="composer-branch-control"
            data-overflow-trigger
            title={triggerLabel}
            variant="ghost"
            size="sm"
            className={className}
          >
            <GitBranch aria-hidden className="size-icon-xs shrink-0" />
            {showRepoLabel && repoLabel ? (
              <OverflowText className="max-w-16 min-w-0 font-medium">
                {repoLabel}
              </OverflowText>
            ) : null}
            <OverflowText className="min-w-0 font-normal">
              {currentBranch}
            </OverflowText>
          </Button>
        </PopoverTrigger>
        <PopoverContent
          align="start"
          aria-label={t("branchSwitch.listLabel")}
          className="flex flex-col gap-2 p-2"
          side="top"
          sideOffset={8}
          width="wide"
        >
          <SearchField
            aria-label={t("branchSwitch.search")}
            autoFocus
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("branchSwitch.searchPlaceholder")}
            size="sm"
            value={query}
            width="full"
          />
          <div className="max-h-64 min-h-0 overflow-y-auto">
            {branchesQuery.isPending ? (
              <div className="text-muted-foreground type-caption flex min-h-24 items-center justify-center gap-2 px-2 py-4">
                <Spinner />
                {t("branchSwitch.loading")}
              </div>
            ) : branchesQuery.isError ? (
              <div className="text-muted-foreground type-caption px-2 py-4 text-center">
                {t("branchSwitch.loadError")}
              </div>
            ) : filteredBranches.length === 0 ? (
              <div className="text-muted-foreground type-caption px-2 py-4 text-center">
                {t("branchSwitch.empty")}
              </div>
            ) : (
              <ul className="grid gap-0.5" role="listbox">
                {filteredBranches.map((branch) => {
                  const selected = branch.name === currentBranch;
                  return (
                    <li key={branch.name}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={selected}
                        disabled={checkoutMutation.isPending}
                        className={cn(
                          "hover:bg-muted focus-visible:ring-ring type-body min-h-row-compact flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left outline-none focus-visible:ring-2 disabled:cursor-default disabled:opacity-60",
                          selected &&
                            "bg-selection-surface text-content-primary",
                        )}
                        onClick={() => void handleSelectBranch(branch.name)}
                      >
                        <GitBranch
                          aria-hidden
                          className="text-muted-foreground size-icon-sm shrink-0"
                          strokeWidth={1.5}
                        />
                        <OverflowText className="min-w-0 flex-1">
                          {branch.name}
                        </OverflowText>
                        {selected ? (
                          <Check
                            aria-hidden
                            className="text-foreground size-icon-sm shrink-0"
                            strokeWidth={1.5}
                          />
                        ) : null}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </PopoverContent>
      </Popover>

      <Dialog
        open={Boolean(pendingBranch && conflicts)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) resetConflictDialog();
        }}
      >
        <DialogContent
          description={t("branchSwitch.conflictDescription")}
          size="md"
          title={t("branchSwitch.conflictTitle")}
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
            <ul className="grid gap-2">
              {(conflicts ?? []).map((file) => (
                <li
                  key={file.path}
                  className="type-body grid grid-cols-[minmax(0,1fr)_auto] items-start gap-3"
                >
                  <OverflowText className="min-w-0">{file.path}</OverflowText>
                  <span className="type-caption flex shrink-0 items-center gap-2 tabular-nums">
                    {file.addedLines > 0 ? (
                      <span className="text-success">+{file.addedLines}</span>
                    ) : null}
                    {file.deletedLines > 0 ? (
                      <span className="text-destructive">
                        -{file.deletedLines}
                      </span>
                    ) : null}
                  </span>
                </li>
              ))}
            </ul>
            <p className="text-muted-foreground type-caption mt-4">
              {t("branchSwitch.commitRequiredHint")}
            </p>
            {commitStep ? (
              <label className="mt-4 grid gap-2">
                <span className="type-caption font-medium">
                  {t("branchSwitch.commitMessageLabel")}
                </span>
                <Textarea
                  autoFocus
                  onChange={(event) => setCommitMessage(event.target.value)}
                  placeholder={t("branchSwitch.commitMessagePlaceholder")}
                  rows={3}
                  value={commitMessage}
                />
              </label>
            ) : null}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="secondary">
                {t("branchSwitch.cancel")}
              </Button>
            </DialogClose>
            {commitStep ? (
              <Button
                type="button"
                disabled={checkoutMutation.isPending}
                onClick={() => void handleCommitAndSwitch()}
              >
                {checkoutMutation.isPending
                  ? t("branchSwitch.switching")
                  : t("branchSwitch.confirmCommitAndSwitch")}
              </Button>
            ) : (
              <Button type="button" onClick={() => setCommitStep(true)}>
                {t("branchSwitch.commitAndSwitch")}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
