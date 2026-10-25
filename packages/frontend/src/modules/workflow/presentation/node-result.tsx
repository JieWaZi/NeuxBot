import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  Component,
  useContext,
  useState,
  useEffect,
  type ReactNode,
} from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  queryOptions,
} from "@tanstack/react-query";
import { getByPath } from "@json-render/core";
import { ArrowUpRight, Files, LayoutPanelTop } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  ReadOnlyMarkdown,
  useOpenWorkbenchContent,
  type ChatContributionScopeProps,
  type WorkbenchContributionProps,
} from "@/modules/chat";
import {
  getWorkflowChatResults,
  listWorkflowNodeResults,
  downloadWorkflowFile,
} from "@/shared/api/generated/sdk.gen";
import { getWorkflowNodeResultOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerApisWorkflowsWorkflowNodeResultReference as ResultReference } from "@/shared/api/generated/types.gen";
import {
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import {
  ResultRenderer,
  type PresentationFile,
} from "@/shared/presentation/result-renderer";
import { catalogDigests } from "@/shared/presentation/catalog-digests.generated";
import type { PageDefinition } from "@/shared/presentation/catalog";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { saveBlob } from "@/shared/platform";
import { WorkflowQueryError } from "../components/query-feedback";
import { NodeResultFiles } from "./node-result-files";
import { OpenResult } from "./result-open-context";

export function resultContentId(ref: ResultReference) {
  return `workflow-node-result:${ref.workflowId}:${ref.runId}:${ref.instanceId}:${ref.attemptId}`;
}
function referenceValid(ref: ResultReference) {
  return [ref.workflowId, ref.runId, ref.instanceId, ref.attemptId].every(
    (value) => typeof value === "string" && /^[a-f0-9]{32}$/.test(value),
  );
}
export function ResultOpenScope({
  scopeKey,
  children,
}: {
  scopeKey: string;
  children: ReactNode;
}) {
  const { t } = useTranslation("workflow");
  const open = useOpenWorkbenchContent(scopeKey);
  const openFile = (ref: ResultReference, fileId?: string, title?: string) => {
    if (!referenceValid(ref)) return;
    open(
      fileId ? `${resultContentId(ref)}:file:${fileId}` : resultContentId(ref),
      title || ref.title || t("resultPage.title"),
      {
        type: "workflow-node-result",
        ids: {
          workflowId: ref.workflowId ?? "",
          runId: ref.runId ?? "",
          instanceId: ref.instanceId ?? "",
          attemptId: ref.attemptId ?? "",
          ...(fileId ? { fileId } : {}),
        },
      },
    );
  };
  return <OpenResult.Provider value={openFile}>{children}</OpenResult.Provider>;
}

export function nodeResultOptions(ref: ResultReference) {
  return getWorkflowNodeResultOptions(
    withLocalErrorSurface({
      path: {
        id: ref.workflowId ?? "",
        runId: ref.runId ?? "",
        instanceId: ref.instanceId ?? "",
        attemptId: ref.attemptId ?? "",
      },
      throwOnError: true,
    }),
  );
}
class PageBoundary extends Component<
  { children: ReactNode; fallback: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

export function NodeResultPage({
  reference,
  compact = false,
  active = true,
  selectedFileId,
}: {
  selectedFileId?: string;
  reference: ResultReference;
  compact?: boolean;
  active?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const open = useContext(OpenResult);
  const query = useQuery({
    ...nodeResultOptions(reference),
    enabled: active && referenceValid(reference),
    staleTime: 30_000,
  });
  useQueryEvents({
    topic: "workflow",
    resource: {
      kind: "workflow",
      id: reference.workflowId || "",
      parentId: reference.runId || "",
    },
    queryKeys: [nodeResultOptions(reference).queryKey],
    active,
    enabled: active && referenceValid(reference),
  });
  const download = useMutation({
    mutationFn: async (file: PresentationFile) => {
      const result = await downloadWorkflowFile(
        withLocalErrorSurface({
          path: {
            id: reference.workflowId ?? "",
            runId: reference.runId ?? "",
          },
          query: {
            instanceId: reference.instanceId,
            attemptId: reference.attemptId,
            fileId: file.id,
          },
          parseAs: "blob",
          throwOnError: true,
        }),
      );
      await saveBlob(result.data as unknown as Blob, file.name ?? "file");
    },
  });
  if (query.isError)
    return (
      <WorkflowQueryError
        error={query.error}
        retrying={query.isFetching}
        onRetry={() => void query.refetch()}
        className="p-4"
      />
    );
  if (!query.data)
    return <ContentLoadingState label={t("loading")} className="p-4" />;
  const data = query.data;
  if (resultContentId(data.reference ?? {}) !== resultContentId(reference))
    return <p role="alert">{t("resultPage.unsupported")}</p>;
  const page = data.page as PageDefinition | undefined;
  if (!data.present)
    return (
      <p className="text-muted-foreground type-body p-4">
        {t("resultPage.unavailable")}
      </p>
    );
  if (data.fileView) {
    return (
      <NodeResultFiles
        key={resultContentId(reference)}
        data={data}
        active={active}
        compact={compact}
        onOpen={(fileId, title) => open(reference, fileId, title)}
        selectedFileId={selectedFileId}
        onDownload={(file) => download.mutate(file)}
        downloadError={download.error}
      />
    );
  }
  if (
    !page ||
    page.catalogVersion !== "workflow-result-v1" ||
    page.formatVersion !== 1 ||
    page.catalogDigest !== catalogDigests["workflow-result-v1"]
  )
    return <p role="alert">{t("resultPage.unsupported")}</p>;
  const paths = Object.fromEntries(
    (data.files ?? []).map((file) => [
      file.field ?? "",
      String(getByPath(data.result, file.field ?? "")),
    ]),
  );
  const authoredPage = page.spec.elements[page.spec.root]?.type === "Page";
  return (
    <div
      className={
        compact
          ? "mt-4 grid min-w-0 gap-3"
          : "grid h-full min-h-0 min-w-0 content-start gap-5 overflow-y-auto p-5"
      }
    >
      {compact || !authoredPage ? (
        <div
          className={`flex items-start gap-3 ${authoredPage ? "justify-end" : "justify-between"}`}
        >
          {!authoredPage ? (
            <div className="grid gap-1">
              <h1 className="type-title font-semibold">
                {page.title || t("resultPage.title")}
              </h1>
              {page.description ? (
                <p className="text-muted-foreground type-body">
                  {page.description}
                </p>
              ) : null}
            </div>
          ) : null}
          {compact ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => open(reference)}
            >
              <ArrowUpRight />
              {t("resultPage.open")}
            </Button>
          ) : null}
        </div>
      ) : null}
      <PageBoundary
        key={resultContentId(reference)}
        fallback={
          <p role="alert" className="text-destructive type-body">
            {t("resultPage.unsupported")}
          </p>
        }
      >
        <ResultRenderer
          page={page}
          result={data.result}
          compact={compact}
          Markdown={ReadOnlyMarkdown}
          files={data.files}
          filePaths={paths}
          download={(file) => download.mutate(file)}
        />
      </PageBoundary>
      {download.error ? (
        <p role="alert" className="text-destructive type-caption">
          {localApiErrorMessage(download.error, t("error"))}
        </p>
      ) : null}
    </div>
  );
}
export function WorkflowResultContent({
  reference,
  active,
  scopeKey,
}: WorkbenchContributionProps) {
  if (reference.type !== "workflow-node-result") return null;
  return (
    <ResultOpenScope scopeKey={scopeKey}>
      <NodeResultPage
        reference={reference.ids}
        active={active}
        selectedFileId={reference.ids.fileId}
      />
    </ResultOpenScope>
  );
}
function resultChatOptions(chatId: string) {
  return queryOptions({
    queryKey: ["workflow-chat-results", chatId],
    queryFn: async () =>
      (
        await getWorkflowChatResults(
          withLocalErrorSurface({ query: { chatId }, throwOnError: true }),
        )
      ).data,
  });
}
export function WorkflowResultMessageScope({
  chatId,
  scopeKey,
  isRunning,
  registerMessages,
  active = true,
}: ChatContributionScopeProps) {
  // 消息与产出顺序无关，独立产出关联事件和执行终态均对账原引用。
  const client = useQueryClient();
  const refs = useQuery({
    ...resultChatOptions(chatId ?? ""),
    enabled: active && Boolean(chatId),
  });
  useQueryEvents({
    topic: "chat",
    resource: { kind: "chat", id: chatId || "" },
    queryKeys: [resultChatOptions(chatId || "").queryKey],
    active,
    enabled: Boolean(chatId),
    eventTypes: [
      "message.changed",
      "assistant.message",
      "tool.message",
      "execution.",
    ],
  });
  useQueryEvents({
    topic: "work-results",
    resource: { kind: "chat", id: chatId || "" },
    queryKeys: [resultChatOptions(chatId || "").queryKey],
    active,
    enabled: Boolean(chatId),
  });
  useQueryEvents({
    topic: "work-board",
    resource: { kind: "chat", id: chatId || "" },
    queryKeys: [resultChatOptions(chatId || "").queryKey],
    active,
    enabled: Boolean(chatId),
  });
  useEffect(() => {
    if (!active || !chatId) return;
    void client.invalidateQueries({
      queryKey: ["workflow-chat-results", chatId],
    });
  }, [active, chatId, client, isRunning]);
  useEffect(
    () =>
      registerMessages((messageId) => {
        const ref = refs.data?.find((item) => item.messageId === messageId);
        return ref ? (
          <ResultOpenScope scopeKey={scopeKey}>
            <NodeResultPage reference={ref} compact active={active} />
          </ResultOpenScope>
        ) : null;
      }),
    [registerMessages, refs.data, scopeKey, active],
  );
  return null;
}

export function NodeOutputCard({
  id,
  runId,
  nodeId,
  title,
  version,
  files = false,
  active = true,
}: {
  id: string;
  runId: string;
  nodeId: string;
  title?: string;
  version?: number;
  files?: boolean;
  active?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const open = useContext(OpenResult);
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string>();
  const [expanded, setExpanded] = useState(false);
  const query = useQuery({
    queryKey: ["workflow-node-results", id, runId, nodeId, version, offset],
    enabled: active,
    queryFn: async () =>
      (
        await listWorkflowNodeResults(
          withLocalErrorSurface({
            path: { id, runId },
            query: { nodeId, offset },
            throwOnError: true,
          }),
        )
      ).data,
  });
  const items = query.data?.items ?? [];
  const chosen =
    items.find((item) => item.reference?.instanceId === selected) ??
    items.find((item) => item.status === "succeeded");
  return (
    <div className="grid gap-0">
      <div className="border-border mx-auto h-5 border-l" aria-hidden="true" />
      <Card variant="resource" className="p-3">
        <button
          type="button"
          className="focus-visible:ring-ring grid w-full gap-2 rounded-lg text-left outline-none focus-visible:ring-2"
          disabled={
            (query.data?.total ?? 0) <= 1 &&
            (!chosen?.reference || chosen.status !== "succeeded")
          }
          onClick={() => {
            if ((query.data?.total ?? 0) > 1) setExpanded(true);
            else if (chosen?.reference) open(chosen.reference);
          }}
        >
          <span className="flex items-center gap-2">
            {files ? (
              <Files className="text-primary size-icon-sm" />
            ) : (
              <LayoutPanelTop className="text-primary size-icon-sm" />
            )}
            <span className="type-label font-medium">
              {title || t("resultPage.title")}
            </span>
            <ArrowUpRight className="text-muted-foreground size-icon-sm ml-auto" />
          </span>
          <span className="text-muted-foreground type-caption">
            {chosen?.status === "succeeded"
              ? t(files ? "resultPage.filesHint" : "resultPage.hint")
              : items.length > 0 &&
                  items.every((item) =>
                    ["failed", "cancelled", "skipped"].includes(
                      item.status ?? "",
                    ),
                  )
                ? t("resultPage.unavailable")
                : t("resultPage.waiting")}
          </span>
        </button>
        {(query.data?.total ?? 0) > 1 ? (
          <div className="mt-2 grid gap-2">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => setExpanded((value) => !value)}
            >
              {t("resultPage.instances", { count: query.data?.total })}
            </Button>
            {expanded ? (
              <div className="grid gap-1">
                {items.map((item) => (
                  <Button
                    key={item.reference?.instanceId}
                    type="button"
                    size="sm"
                    variant={
                      selected === item.reference?.instanceId
                        ? "secondary"
                        : "ghost"
                    }
                    disabled={item.status !== "succeeded"}
                    onClick={() => {
                      setSelected(item.reference?.instanceId);
                      if (item.reference) open(item.reference);
                    }}
                  >
                    {item.label || item.path} · {t(`status.${item.status}`)}
                  </Button>
                ))}
                <div className="flex justify-between">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={offset === 0}
                    onClick={() => setOffset(Math.max(0, offset - 20))}
                  >
                    {t("resultPage.previous")}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={!query.data?.nextOffset}
                    onClick={() => setOffset(query.data?.nextOffset ?? 0)}
                  >
                    {t("resultPage.next")}
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        ) : null}
        {query.error ? (
          <WorkflowQueryError
            error={query.error}
            retrying={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        ) : null}
      </Card>
    </div>
  );
}
