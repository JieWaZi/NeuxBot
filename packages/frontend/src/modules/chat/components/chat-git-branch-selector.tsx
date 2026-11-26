import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, GitBranch, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  listWorkspaceGitBranches,
  previewWorkspaceGitCheckout,
  runWorkspaceGitAction,
  type ScopedGitStatus,
  type WorkspaceGitCheckoutConflict,
  type WorkspaceGitTarget,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { OverflowText } from "@/shared/ui/overflow-text";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";
import { toast } from "@/shared/ui/sonner";
import { invalidateChatGit } from "../model/chat-git";

export function ChatGitBranchSelector({
  target,
  status,
  disabled,
  showRepositoryPath,
}: {
  target: WorkspaceGitTarget & { repository: string };
  status: ScopedGitStatus;
  disabled: boolean;
  showRepositoryPath: boolean;
}) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [conflict, setConflict] = useState<{
    branch: string;
    files: WorkspaceGitCheckoutConflict[];
  }>();
  const { chatId, root, repository } = target;
  const currentBranch = status.branch || status.head?.slice(0, 8) || "HEAD";
  const branches = useQuery({
    queryKey: ["scoped-git-branches", chatId, root, repository],
    queryFn: () => listWorkspaceGitBranches(root, { chatId, repository }),
    enabled: open,
  });
  const search = query.trim();
  const items = (branches.data?.branches ?? []).filter((branch) =>
    branch.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  const action = useMutation({
    mutationFn: async ({
      branch,
      create,
    }: {
      branch: string;
      create: boolean;
    }) => {
      if (!create) {
        const preview = await previewWorkspaceGitCheckout(root, branch, {
          chatId,
          repository,
        });
        if (!preview.canCheckout) {
          setOpen(false);
          setConflict({ branch, files: preview.conflicts });
          return;
        }
      }
      const result = await runWorkspaceGitAction({
        ...target,
        action: create ? "create_branch" : "checkout",
        branch,
        expectedBranch: status.branch ?? "",
        expectedHead: status.head ?? "",
      });
      if (result.failedStep) throw new Error(t("summary.git.actionFailed"));
      setOpen(false);
    },
    onError: (error) => toast.error(error.message),
    onSettled: () => invalidateChatGit(queryClient, root, repository),
  });

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
            size="row"
            variant="ghost"
            title={currentBranch}
            aria-label={
              showRepositoryPath
                ? `${currentBranch} (${repository})`
                : undefined
            }
            disabled={disabled}
            className="w-full min-w-0"
          >
            <GitBranch />
            <OverflowText className="min-w-0 flex-1 text-left">
              {currentBranch}
            </OverflowText>
            <ChevronDown />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          align="start"
          aria-label={t("branchSwitch.listLabel")}
          width="wide"
        >
          <SearchField
            aria-label={t("branchSwitch.search")}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("branchSwitch.searchPlaceholder")}
            size="sm"
            value={query}
            width="full"
          />
          <div className="mt-2 max-h-64 overflow-y-auto">
            {branches.isError ? (
              <Button
                variant="ghost"
                size="row"
                onClick={() => void branches.refetch()}
              >
                {t("branchSwitch.loadError")}
              </Button>
            ) : branches.isPending ? (
              <p role="status" className="text-content-muted type-caption p-2">
                {t("branchSwitch.loading")}
              </p>
            ) : (
              <>
                {items.map((branch) => (
                  <Button
                    key={branch.name}
                    size="row"
                    variant="ghost"
                    disabled={action.isPending || branch.isCurrent}
                    onClick={() =>
                      action.mutate({ branch: branch.name, create: false })
                    }
                    className="w-full min-w-0"
                  >
                    {branch.isCurrent ? <Check /> : <GitBranch />}
                    <OverflowText className="min-w-0">
                      {branch.name}
                    </OverflowText>
                  </Button>
                ))}
                {search && !items.some((branch) => branch.name === search) ? (
                  <Button
                    size="row"
                    variant="ghost"
                    className="w-full min-w-0"
                    disabled={action.isPending}
                    onClick={() =>
                      action.mutate({ branch: search, create: true })
                    }
                  >
                    <Plus />
                    <OverflowText className="min-w-0">
                      {t("summary.git.createBranch", { branch: search })}
                    </OverflowText>
                  </Button>
                ) : null}
              </>
            )}
          </div>
        </PopoverContent>
      </Popover>
      <Dialog
        open={Boolean(conflict)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setConflict(undefined);
        }}
      >
        <DialogContent title={t("summary.git.checkoutConflict")} size="sm">
          <div className="grid gap-2 overflow-y-auto px-5 pb-4">
            <p className="text-content-muted type-caption wrap-anywhere">
              {repository} → {conflict?.branch}
            </p>
            {conflict?.files.map((file) => (
              <p key={file.path} className="type-ui wrap-anywhere">
                {file.path}
              </p>
            ))}
          </div>
          <DialogFooter>
            <Button size="md" onClick={() => setConflict(undefined)}>
              {t("common:close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
