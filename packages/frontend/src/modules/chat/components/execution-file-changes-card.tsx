import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FilePenLine } from "lucide-react";
import { useContext, useEffect, useRef, useState } from "react";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { observeViewportActivation } from "@/shared/lib/viewport-activation";
import { useTranslation } from "react-i18next";

import { useFileReferenceContext } from "@/shared/workspace/file-reference-link";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { getChatMessageFileChangesOptions } from "../api/chat-queries";
import type { AllyChatMessage } from "../runtime/chat-projection";
import type { ChatFileChangeOpenRequest } from "./chat-thread";

/** 消息只持有成果引用，摘要以版本作为缓存身份，文件内容在打开时读取。 */
export function ExecutionFileChangesCard({
  message,
  chatId,
  visible = false,
  onOpenFile,
}: {
  message: AllyChatMessage;
  chatId?: string;
  visible?: boolean;
  onOpenFile?: (file: ChatFileChangeOpenRequest) => void;
}) {
  const { t } = useTranslation("chat");
  const scope = useFileReferenceContext();
  const active = useContext(ContentVisibility);
  const owner = chatId ?? scope.chatId;
  const reference = message.fileChangeRecord;
  // 授权范围与采集状态不是编辑证据；确认有净变更后才展示文件成果。
  const empty =
    reference?.state === "pending" || (reference?.totalFiles ?? 0) <= 0;
  const element = useRef<HTMLElement>(null);
  const [activated, setActivated] = useState(false);
  useEffect(() => {
    if (
      !reference ||
      empty ||
      !visible ||
      !active ||
      activated ||
      !element.current
    )
      return;
    return observeViewportActivation(element.current, () => setActivated(true));
  }, [visible, active, activated, empty, reference]);
  const [expanded, setExpanded] = useState(false);
  const options = getChatMessageFileChangesOptions({
    path: { chatId: owner ?? "", messageId: message.id ?? "" },
  });
  const summary = useQuery({
    ...options,
    queryKey: [
      {
        ...options.queryKey[0],
        tags: [reference?.recordId ?? "", String(reference?.version ?? 0)],
      },
    ],
    enabled:
      visible &&
      active &&
      activated &&
      !empty &&
      Boolean(owner && message.id && reference) &&
      reference?.state !== "pending",
    staleTime: Infinity,
    retry: false,
    throwOnError: false,
  });
  if (!visible || !reference || empty) return null;
  const files = summary.data?.files ?? [];
  // 明细按需读取不改变采集状态；切换会话时保留已确认的成果与文件数。
  const state = summary.data?.state ?? reference.state;
  const count = summary.data?.totalFiles ?? reference.totalFiles ?? 0;
  const incomplete =
    state === "partial" ||
    state === "incomplete" ||
    summary.data?.complete === false;
  if (count <= 0) return null;
  const rows = expanded ? files : files.slice(0, 3);
  // 仅在列表完整且每项行数已知时汇总，未知统计不能当作零。
  const totals =
    files.length > 0 &&
    files.length === count &&
    !incomplete &&
    !summary.data?.truncated &&
    files.every(
      (file) => file.added !== undefined && file.deleted !== undefined,
    )
      ? files.reduce<{ added: number; deleted: number }>(
          (total, file) => ({
            added: total.added + (file.added ?? 0),
            deleted: total.deleted + (file.deleted ?? 0),
          }),
          { added: 0, deleted: 0 },
        )
      : undefined;
  const openFile = (file: (typeof files)[number]) => {
    const path = file.absolutePath ?? file.path ?? "";
    const root = summary.data?.roots?.find((root) => root.id === file.rootId);
    onOpenFile?.({
      messageId: message.id ?? "",
      path,
      name: path.replaceAll("\\", "/").split("/").at(-1) ?? path,
      // 精确文件根与文件身份相同，差异仍按原消息读取，不将文件路径作为目录范围。
      rootPath: root?.path === path ? undefined : root?.path,
    });
  };
  const viewChangesAction = (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      className="ml-auto shrink-0 whitespace-nowrap"
      disabled={!files.length || state === "pending" || summary.isError}
      onClick={
        files.length === 1
          ? () => {
              const first = files[0];
              if (first) openFile(first);
            }
          : undefined
      }
    >
      {t("fileChanges.viewChanges")}
    </Button>
  );
  return (
    <Card
      asChild
      className="border-border-subtle bg-surface-scene mt-3 gap-0 overflow-hidden border py-0"
    >
      <section
        ref={element}
        data-work-result-body={
          (summary.data && !summary.isPending) ||
          state === "unavailable" ||
          state === "incomplete"
            ? true
            : undefined
        }
        data-file-change-record-id={reference.recordId}
        data-file-change-record-version={reference.version}
        aria-label={t("fileChanges.title", { count })}
      >
        <div className="flex flex-wrap items-center gap-3 px-4 py-3">
          <span className="text-muted-foreground shrink-0 rounded-lg p-2">
            <FilePenLine className="size-icon-lg" aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1 basis-24">
            <span className="type-label font-medium">
              {state === "unavailable" || summary.isError
                ? t("fileChanges.unavailable")
                : state === "incomplete" || (state === "partial" && count === 0)
                  ? t("fileChanges.incomplete")
                  : state === "pending"
                    ? t("fileChanges.pending")
                    : t("fileChanges.title", { count })}
            </span>
            {totals ? (
              <div className="type-body mt-0.5 flex gap-2 tabular-nums">
                <span className="text-success">+{totals.added}</span>
                <span className="text-destructive">-{totals.deleted}</span>
              </div>
            ) : null}
            {incomplete ? (
              <span className="type-caption text-muted-foreground block">
                {t("fileChanges.partial")}
              </span>
            ) : null}
          </div>
          {onOpenFile ? (
            files.length > 1 ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  {viewChangesAction}
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="end"
                  aria-label={t("fileChanges.viewChanges")}
                >
                  {files.map((file) => (
                    <DropdownMenuItem
                      key={`${file.rootId}:${file.path}`}
                      onSelect={() => openFile(file)}
                      title={file.absolutePath ?? file.path}
                    >
                      <span className="min-w-0 truncate">
                        {file.path || file.absolutePath}
                      </span>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              viewChangesAction
            )
          ) : null}
        </div>
        {summary.isError ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="mx-3 mb-2"
            onClick={() => void summary.refetch()}
          >
            {t("workspace.retry")}
          </Button>
        ) : null}
        {rows.length ? (
          <ul className="border-border-subtle border-t py-1">
            {rows.map((file) => {
              const path = file.absolutePath ?? file.path ?? "";
              const displayPath = file.path || path;
              const separator = Math.max(
                displayPath.lastIndexOf("/"),
                displayPath.lastIndexOf("\\"),
              );
              const directory = displayPath.slice(0, separator + 1);
              const name = displayPath.slice(separator + 1);
              const content = (
                <>
                  <span
                    className="type-body flex min-w-0 items-baseline text-left"
                    title={path}
                  >
                    {directory ? (
                      <span className="text-muted-foreground truncate">
                        {directory}
                      </span>
                    ) : null}
                    <span className="max-w-full min-w-0 shrink-0 truncate">
                      {name}
                    </span>
                  </span>
                  <span className="type-body flex shrink-0 gap-2 tabular-nums">
                    {file.added !== undefined ? (
                      <span className="text-success">+{file.added}</span>
                    ) : null}
                    {file.deleted !== undefined ? (
                      <span className="text-destructive">-{file.deleted}</span>
                    ) : null}
                    {file.content !== "text" ? (
                      <span className="type-caption text-muted-foreground">
                        {t(
                          `fileChanges.${file.content === "binary" ? "binary" : file.content === "oversized" ? "oversized" : "unreadable"}`,
                        )}
                      </span>
                    ) : null}
                  </span>
                </>
              );
              return (
                <li key={`${file.rootId}:${file.path}`}>
                  {onOpenFile ? (
                    <button
                      type="button"
                      className="hover:bg-card-hover focus-visible:ring-ring min-h-control-md grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
                      aria-label={t("fileChanges.openFile", { path })}
                      onClick={() => openFile(file)}
                    >
                      {content}
                    </button>
                  ) : (
                    <div className="min-h-control-md grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-2">
                      {content}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        ) : null}
        {files.length > 3 ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="mx-3 mb-2"
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded
              ? t("fileChanges.showLess")
              : t("fileChanges.showMore", {
                  count: files.length - rows.length,
                })}
            <ChevronRight
              className={expanded ? "rotate-90" : ""}
              aria-hidden="true"
            />
          </Button>
        ) : null}
        {summary.data?.truncated ? (
          <p className="type-caption text-muted-foreground px-3 pb-2">
            {t("fileChanges.truncated")}
          </p>
        ) : null}
      </section>
    </Card>
  );
}
