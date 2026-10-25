import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useQuery } from "@tanstack/react-query";
import { FileDiff, FileText, ListTree, Waypoints } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  getWorkspaceGitChanges,
  listChatWorkspaceFiles,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import type { WorkspaceFile } from "@/shared/workspace/types";

import { workspaceFilesWithGenerated } from "../model/chat-workspace-files";

const previewFileCount = 3;

type ChatSummaryProps = {
  directoryLabel?: string;
  chatId?: string;
  chatFiles: readonly WorkspaceFile[];
  gitRoots: readonly string[];
  onOpenChanges: (root: string) => void;
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
  onOpenChanges,
  onOpenFile,
  onViewAllFiles,
  renderTeamMembers,
  workspaceDirectory,
}: ChatSummaryProps) {
  const { t } = useTranslation("chat");
  const [open, setOpen] = useState(false);
  const roots = [...new Set(gitRoots.filter(Boolean))];
  const changes = useQuery({
    queryKey: ["chat-summary-git-changes", chatId, roots],
    queryFn: () =>
      Promise.all(
        roots.map(async (root) => ({
          root,
          changes: await getWorkspaceGitChanges(root),
        })),
      ),
    enabled: open && roots.length > 0,
  });
  const generatedFiles = useQuery({
    queryKey: ["chat-workspace-files", chatId, workspaceDirectory],
    queryFn: () => {
      if (!chatId) throw new Error("Chat 尚未就绪");
      return listChatWorkspaceFiles(chatId, workspaceDirectory);
    },
    enabled: open && Boolean(chatId && workspaceDirectory),
  });
  const workspaceEvents = useWorkspaceQueryEvents(
    chatId,
    [...roots, workspaceDirectory],
    ["chat-summary-git-changes", "chat-workspace-files"],
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
            {roots.length ? (
              <div className="grid min-w-0 gap-3">
                {roots.map((root) => {
                  const name =
                    directoryLabel ??
                    root.split(/[\\/]/).filter(Boolean).at(-1) ??
                    root;
                  const status = changes.data?.find(
                    (item) => item.root === root,
                  )?.changes;
                  return (
                    <div key={root} className="grid gap-2">
                      <h2
                        className="text-muted-foreground type-body truncate"
                        title={root}
                      >
                        {name}
                      </h2>
                      <Button
                        size="row"
                        variant="ghost"
                        className="w-full min-w-0"
                        aria-label={t("summary.directoryChanges", {
                          directory: name,
                        })}
                        title={root}
                        onClick={() => {
                          setOpen(false);
                          onOpenChanges(root);
                        }}
                      >
                        <FileDiff className="shrink-0" />
                        <span className="text-content-primary min-w-0 flex-1 text-left">
                          {t("summary.changes")}
                        </span>
                        {changes.isPending ? (
                          <span className="text-muted-foreground type-ui font-normal">
                            {t("workspace.changesLoading")}
                          </span>
                        ) : changes.isError || workspaceEvents.error ? (
                          <span className="text-destructive type-ui font-normal">
                            {t("workspace.changesLoadError")}
                          </span>
                        ) : status?.isGitRepo ? (
                          <span className="shrink-0 font-normal tabular-nums">
                            <span className="text-success">
                              +{status.totalAddedLines}
                            </span>{" "}
                            <span className="text-destructive">
                              -{status.totalDeletedLines}
                            </span>
                          </span>
                        ) : (
                          <span className="text-muted-foreground type-ui shrink-0 font-normal">
                            {t("summary.notGit")}
                          </span>
                        )}
                      </Button>
                    </div>
                  );
                })}
              </div>
            ) : (
              <p className="text-muted-foreground type-ui py-2">
                {directoryLabel ?? t("summary.noProjectDirectory")}
              </p>
            )}
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
