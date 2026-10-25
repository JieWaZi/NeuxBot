import { Suspense, useState } from "react";
import { getByPath } from "@json-render/core";
import { ArrowUpRight, Download, Folders } from "lucide-react";
import { useTranslation } from "react-i18next";
import { WorkspaceDetailSplit, WorkspaceFileViewer } from "@/modules/chat";
import type { GithubComJieWaZiNeuxBotServerApisWorkflowsWorkflowNodeResultResponse as ResultResponse } from "@/shared/api/generated/types.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import type { PresentationFile } from "@/shared/presentation/result-renderer";
import type { WorkspaceFileView } from "@/shared/workspace/workspace-tab";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import { PageAction } from "@/shared/ui/resource-page";
import { Card } from "@/shared/ui/card";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";

export function NodeResultFiles({
  data,
  active,
  compact,
  onOpen,
  onDownload,
  downloadError,
  selectedFileId,
}: {
  selectedFileId?: string;
  data: ResultResponse;
  active: boolean;
  compact: boolean;
  onOpen: (fileId?: string, title?: string) => void;
  onDownload: (file: PresentationFile) => void;
  downloadError: unknown;
}) {
  const { t } = useTranslation("workflow");
  const files = data.files ?? [];
  const [selectedId, setSelectedId] = useState<string | undefined>(
    selectedFileId,
  );
  const [listOpen, setListOpen] = useState(true);
  const [views, setViews] = useState<Map<string, WorkspaceFileView>>(
    () => new Map(),
  );
  const selected =
    files.find((file) => file.id === selectedId) ??
    files.find((file) => file.available) ??
    files[0];
  const rawPath = selected
    ? selected.field
      ? getByPath(data.result, selected.field)
      : data.result
    : undefined;
  const path =
    typeof rawPath === "string" &&
    data.workingDirectory &&
    rawPath.startsWith(`${data.workingDirectory}/`)
      ? rawPath.slice(data.workingDirectory.length + 1)
      : rawPath;
  const title = data.fileView?.title ?? t("resultPage.filesTitle");
  if (compact) {
    return (
      <Card variant="resource" className="mt-4 p-3">
        <button
          type="button"
          className="focus-visible:ring-ring grid w-full gap-2 rounded-lg text-left outline-none focus-visible:ring-2"
          onClick={() => onOpen()}
        >
          <span className="flex min-w-0 items-center gap-2">
            <WorkspaceFileTypeIcon
              isDirectory={false}
              name={files.length === 1 ? (files[0]?.name ?? "") : ""}
              size={16}
            />
            <span className="type-label min-w-0 flex-1 font-medium break-words">
              {title}
            </span>
            <ArrowUpRight
              className="text-muted-foreground size-icon-sm shrink-0"
              strokeWidth={1.5}
            />
          </span>
          <span className="text-muted-foreground type-caption">
            {files.length === 1
              ? files[0]?.name
              : t("resultPage.filesCount", { count: files.length })}
          </span>
        </button>
      </Card>
    );
  }
  const list = (
    <div className="grid h-full min-h-0 content-start gap-2 overflow-y-auto p-3">
      {files.length === 0 ? (
        <p className="text-muted-foreground type-body">
          {t("resultPage.noFiles")}
        </p>
      ) : null}
      {files.map((file) => (
        <div
          key={file.id}
          className="bg-muted/60 flex min-w-0 items-center gap-2 rounded-md p-2"
        >
          <button
            type="button"
            aria-pressed={selected?.id === file.id}
            className="focus-visible:ring-ring flex min-w-0 flex-1 items-center gap-2 rounded-sm text-left outline-none focus-visible:ring-2"
            disabled={!file.available}
            onClick={() => setSelectedId(file.id)}
          >
            <WorkspaceFileTypeIcon
              isDirectory={false}
              name={file.name ?? ""}
              size={16}
            />
            <span className="grid min-w-0 gap-1">
              <span className="type-body break-words">{file.name}</span>
              <span className="text-muted-foreground type-caption">
                {file.available
                  ? t("resultPage.fileSize", {
                      size: ((file.size ?? 0) / 1024).toLocaleString(
                        undefined,
                        { maximumFractionDigits: 1 },
                      ),
                    })
                  : t("outputFileUnavailable")}
              </span>
            </span>
          </button>
          <PageAction
            type="button"
            hitArea="comfortable"
            variant="ghost"
            disabled={!file.available}
            aria-label={t("resultPage.downloadFile", { name: file.name })}
            onClick={() => onDownload(file)}
          >
            <Download strokeWidth={1.5} />
          </PageAction>
        </div>
      ))}
    </div>
  );
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      {downloadError ? (
        <p role="alert" className="text-destructive type-body p-3">
          {localApiErrorMessage(downloadError, t("error"))}
        </p>
      ) : null}
      {selected &&
      selected.available &&
      typeof path === "string" &&
      data.workingDirectory &&
      data.chatId ? (
        <WorkspaceDetailSplit
          sidebarOpen={files.length > 1 && listOpen}
          resizeLabel={t("resultPage.resizeFiles")}
          main={
            <Suspense
              fallback={
                <ContentLoadingState label={t("loading")} className="p-4" />
              }
            >
              <WorkspaceFileViewer
                key={selected.id}
                active={active}
                readOnly
                path={path}
                rootPath={data.workingDirectory}
                scopeChatId={data.chatId}
                fileView={views.get(selected.id ?? "") ?? "preview"}
                onFileViewChange={(view) =>
                  setViews((current) =>
                    new Map(current).set(selected.id ?? "", view),
                  )
                }
                toolbarAccessory={
                  files.length > 1 ? (
                    <PageAction
                      type="button"
                      hitArea="comfortable"
                      variant="ghost"
                      aria-label={t("resultPage.toggleFiles")}
                      aria-pressed={listOpen}
                      onClick={() => setListOpen((value) => !value)}
                    >
                      <Folders strokeWidth={1.5} />
                    </PageAction>
                  ) : undefined
                }
              />
            </Suspense>
          }
          sidebar={list}
        />
      ) : (
        <div className="min-h-0 overflow-y-auto">
          <div className="grid gap-1 p-4">
            <h1 className="type-title font-semibold">{title}</h1>
            {data.fileView?.description ? (
              <p className="text-muted-foreground type-body">
                {data.fileView.description}
              </p>
            ) : null}
          </div>
          {list}
        </div>
      )}
    </div>
  );
}
