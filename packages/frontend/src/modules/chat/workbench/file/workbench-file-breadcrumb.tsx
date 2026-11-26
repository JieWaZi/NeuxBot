import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { workspaceDirectoryOptions } from "../../components/workspace-file-browser";
import { gitStatusOptions } from "../../model/chat-git";
import {
  matchesWorkspacePath,
  workspacePathAtRoot,
} from "../../model/project-files-scope";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { fileName } from "@/shared/workspace/file-reference";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";

export type BreadcrumbDirectory = { rootPath: string; path: string };

type OpenBreadcrumbFile = (
  file: { name: string; path: string },
  rootPath: string,
) => void;

export function WorkbenchFileBreadcrumb({
  active,
  file,
  scopeChatId,
  directoryScope,
  onRevealDirectory,
  onOpenFile,
}: {
  active: boolean;
  file: {
    path: string;
    canonicalPath: string;
    rootPath: string;
    chatId?: string;
  };
  scopeChatId?: string;
  directoryScope: (path: string) => BreadcrumbDirectory | undefined;
  onRevealDirectory: (path: string, rootPath: string) => void;
  onOpenFile?: OpenBreadcrumbFile;
}) {
  const { t } = useTranslation("chat");
  const scrollArea = useRef<HTMLElement>(null);
  const { data: repository } = useQuery({
    ...gitStatusOptions(file.chatId, file.rootPath),
    enabled: active,
    staleTime: 60_000,
    retry: false,
    select: (status) => {
      const repositories = status.repositories?.length
        ? status.repositories
        : [status.root];
      return repositories
        .filter(
          (root): root is string =>
            Boolean(root) &&
            matchesWorkspacePath(file.canonicalPath, root, true),
        )
        .sort((left, right) => right.length - left.length)[0];
    },
  });
  const root = (repository ?? file.rootPath)
    .replaceAll("\\", "/")
    .replace(/\/+$/, "");
  const relative = repository
    ? file.canonicalPath
        .replaceAll("\\", "/")
        .slice(root.length)
        .replace(/^\/+/, "")
    : file.path.replaceAll("\\", "/");
  const segments = relative.split("/").filter(Boolean);
  const crumbs = [
    { label: fileName(root) || "/", path: root || "/" },
    ...segments.map((label, index) => ({
      label,
      path: `${root}/${segments.slice(0, index + 1).join("/")}`,
    })),
  ];

  useEffect(() => {
    const area = scrollArea.current;
    if (active && area) area.scrollLeft = area.scrollWidth;
  }, [active, root, relative]);

  return (
    <nav
      ref={scrollArea}
      aria-label={t("workspace.fileBreadcrumb")}
      className="scrollbar-hidden text-muted-foreground type-ui flex h-full min-w-0 flex-1 overflow-x-auto"
    >
      <ol className="flex w-max min-w-full items-center">
        {crumbs.map(({ label, path }, index) => {
          const current = index === crumbs.length - 1;
          const directory = current ? undefined : directoryScope(path);
          return (
            <li
              key={path}
              aria-current={current ? "page" : undefined}
              className="flex min-w-0 shrink-0 items-center"
            >
              <Tooltip>
                <TooltipTrigger asChild>
                  {directory ? (
                    <button
                      type="button"
                      className="hover:bg-muted hover:text-foreground focus-visible:ring-ring max-w-40 truncate rounded-sm px-0.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
                      onClick={() =>
                        onRevealDirectory(path, directory.rootPath)
                      }
                    >
                      {label}
                    </button>
                  ) : (
                    <span
                      className={
                        current
                          ? "text-foreground max-w-40 truncate px-0.5 font-medium"
                          : "max-w-40 truncate px-0.5"
                      }
                    >
                      {label}
                    </span>
                  )}
                </TooltipTrigger>
                <TooltipContent>{path}</TooltipContent>
              </Tooltip>
              {!current &&
                (active && directory && onOpenFile ? (
                  <FolderContentsMenu
                    key={JSON.stringify([
                      file.canonicalPath,
                      scopeChatId,
                      directory.rootPath,
                      directory.path,
                    ])}
                    label={label}
                    directory={directory}
                    scopeChatId={scopeChatId}
                    currentFile={file.canonicalPath}
                    onOpenFile={onOpenFile}
                  />
                ) : (
                  <ChevronRight
                    aria-hidden="true"
                    className="text-muted-foreground/70 size-icon-sm mx-0.5 shrink-0"
                    strokeWidth={1.5}
                  />
                ))}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function FolderContentsMenu({
  label,
  directory,
  scopeChatId,
  currentFile,
  onOpenFile,
}: {
  label: string;
  directory: BreadcrumbDirectory;
  scopeChatId?: string;
  currentFile: string;
  onOpenFile: OpenBreadcrumbFile;
}) {
  const { t } = useTranslation("chat");
  const [open, setOpen] = useState(false);
  const browseLabel = t("workspace.browseBreadcrumbDirectory", { name: label });

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={browseLabel}
              className="text-muted-foreground/70 hover:bg-muted hover:text-foreground data-[state=open]:bg-muted data-[state=open]:text-foreground focus-visible:ring-ring size-icon-lg grid shrink-0 place-items-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-inset"
            >
              <ChevronRight
                aria-hidden="true"
                className="size-icon-sm"
                strokeWidth={1.5}
              />
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>{browseLabel}</TooltipContent>
      </Tooltip>
      {open && (
        <FolderContents
          label={label}
          directory={directory}
          scopeChatId={scopeChatId}
          currentFile={currentFile}
          onOpenFile={onOpenFile}
        />
      )}
    </DropdownMenu>
  );
}

function FolderContents({
  label,
  directory,
  scopeChatId,
  currentFile,
  onOpenFile,
}: {
  label: string;
  directory: BreadcrumbDirectory;
  scopeChatId?: string;
  currentFile: string;
  onOpenFile: OpenBreadcrumbFile;
}) {
  const { t } = useTranslation("chat");
  const { rootPath, path: initialPath } = directory;
  const [path, setPath] = useState(initialPath);
  const contents = useQuery(
    workspaceDirectoryOptions(scopeChatId, rootPath, path),
  );
  const parent =
    path === initialPath
      ? undefined
      : path.slice(0, Math.max(0, path.lastIndexOf("/")));

  return (
    <DropdownMenuContent
      align="start"
      onKeyDown={(event) => {
        if (event.key !== "ArrowLeft" || parent === undefined) return;
        event.preventDefault();
        event.stopPropagation();
        setPath(parent);
      }}
    >
      {parent !== undefined && (
        <>
          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault();
              setPath(parent);
            }}
          >
            <ArrowLeft />
            {t("workspace.backToBreadcrumbDirectory", {
              name: fileName(parent) || label,
            })}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
        </>
      )}
      {contents.isPending ? (
        <DropdownMenuItem disabled>
          {t("workspace.filesLoading")}
        </DropdownMenuItem>
      ) : !contents.data?.length && !contents.isError ? (
        <DropdownMenuItem disabled>
          {t("workspace.projectDirectoryEmpty")}
        </DropdownMenuItem>
      ) : (
        contents.data?.map((entry) => (
          <DropdownMenuItem
            key={entry.relativePath}
            aria-current={
              !entry.isDirectory &&
              matchesWorkspacePath(
                currentFile,
                workspacePathAtRoot(rootPath, entry.relativePath),
              )
                ? "page"
                : undefined
            }
            onSelect={(event) => {
              if (entry.isDirectory) {
                event.preventDefault();
                setPath(entry.relativePath);
              } else {
                onOpenFile(
                  { name: entry.name, path: entry.relativePath },
                  rootPath,
                );
              }
            }}
          >
            <WorkspaceFileTypeIcon
              name={entry.name}
              isDirectory={entry.isDirectory}
              size={14}
            />
            <span className="min-w-0 flex-1 truncate">{entry.name}</span>
            {entry.isDirectory && <ChevronRight />}
          </DropdownMenuItem>
        ))
      )}
      {contents.isError && (
        <DropdownMenuItem
          onSelect={(event) => {
            event.preventDefault();
            void contents.refetch();
          }}
        >
          {t("workspace.filesLoadError")} · {t("workspace.retry")}
        </DropdownMenuItem>
      )}
    </DropdownMenuContent>
  );
}
