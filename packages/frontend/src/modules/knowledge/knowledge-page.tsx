import {
  canUploadKnowledge,
  hasKnowledgeCapability,
} from "@/shared/api/knowledge/knowledge-adapter-types";
import { KnowledgeLibrarySettings } from "./knowledge-library-settings";
import { CreateLibrary } from "./knowledge-library-dialogs";
import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import {
  knowledgeAdapter,
  type KnowledgeUploadContext,
} from "@/shared/api/knowledge";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { PageHeader } from "@/shared/ui/page-header";
import { Separator } from "@/shared/ui/separator";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { Pagination } from "@/shared/ui/pagination";
import {
  createKnowledgeEditorController,
  type KnowledgeEditorController,
} from "./knowledge-editor-controller";
import { buildExecutionDefaults } from "./knowledge-execution-config";
import {
  createKnowledgeUploadQueue,
  type KnowledgeUploadQueue,
} from "./knowledge-upload-queue";
import { KnowledgeUploadDialog as UploadSources } from "./knowledge-upload-dialog";
import {
  type KnowledgeOrganizeIntent,
  createKnowledgeOrganizeIntent,
  KnowledgeOrganizeDialog,
} from "./knowledge-organize-dialog";
import { KnowledgeLibraryActions } from "./knowledge-library-actions";
import {
  KnowledgeGraphLoading,
  KnowledgeDocumentsLoading,
  KnowledgeWikiLoading,
} from "./knowledge-loading";
const RawView = lazy(() =>
  import("./knowledge-raw-view").then((module) => ({
    default: module.RawView,
  })),
);
const KnowledgeDocumentReader = lazy(() =>
  import("./knowledge-document-reader").then((module) => ({
    default: module.KnowledgeDocumentReader,
  })),
);
import { LibraryList } from "./knowledge-library";
import type { createLibraryDeleter } from "./delete-library";
import { useKnowledgeWrites } from "./knowledge-writes";
import { useKnowledgeWorkspaceQueries } from "./knowledge-workspace-queries";
import { knowledgeKeys, toLibrary, toSource } from "./knowledge-queries";
import {
  readRoute,
  isOrganizableSource,
  type Source,
  type Route,
  type KnowledgeReadState,
} from "./knowledge-model";
import { isKnowledgeJobBusy, resourceKey } from "./knowledge-transfer";
import { useKnowledgeSourcePaginationLabels } from "./use-knowledge-source-pagination";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { useCopy } from "./use-copy";

const KnowledgeRemoteAddDialog = lazy(() =>
  import("./knowledge-remote-add-dialog").then((module) => ({
    default: module.KnowledgeRemoteAddDialog,
  })),
);
const KnowledgeWikiView = lazy(() =>
  import("./knowledge-wiki-view").then((module) => ({
    default: module.KnowledgeWikiView,
  })),
);

const KnowledgeGraphView = lazy(() =>
  import("./knowledge-graph-view").then((module) => ({
    default: module.KnowledgeGraphView,
  })),
);

function reading(query: {
  isPending: boolean;
  isFetching: boolean;
  error: unknown;
  data: unknown;
  refetch: () => Promise<unknown>;
}): KnowledgeReadState {
  return {
    pending: query.isPending,
    error: query.error,
    refreshing: query.isFetching && !query.isPending,
    stale: query.data !== undefined && Boolean(query.error),
    retry: () => void query.refetch(),
  };
}
/** 库详情装配文档、Wiki 与图谱，各视图按可见范围读取。 */
export function KnowledgePage() {
  const t = useCopy();
  const paginationLabels = useKnowledgeSourcePaginationLabels();
  const [params] = useSearchParams();
  const path = useParams();
  const navigate = useNavigate();
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [deleteLibrary] = useState(() => {
    let deleter: Promise<ReturnType<typeof createLibraryDeleter>> | undefined;
    return async (
      ...args: Parameters<ReturnType<typeof createLibraryDeleter>>
    ) => {
      deleter ??= import("./delete-library")
        .then((module) => module.createLibraryDeleter())
        .catch((cause: unknown) => {
          deleter = undefined;
          throw cause;
        });
      return (await deleter)(...args);
    };
  });
  const route = readRoute(params);
  if (path.libraryId) {
    route.libraryId = path.libraryId;
    route.view =
      params.get("view") === "graph"
        ? "graph"
        : params.get("view") === "raw"
          ? "raw"
          : "wiki";
  }
  const detail = route.view !== "library" && Boolean(route.libraryId);
  const ref = {
    backend: detail ? params.get("backend") || "local" : "local",
    connectionId: detail ? params.get("connection") || "local" : "local",
    knowledgeBaseId: route.libraryId,
  };
  const adapter = knowledgeAdapter(ref);
  const identity = resourceKey(ref);
  const root = knowledgeKeys.resource(ref);
  const location =
    params.get("location") === "local"
      ? "local"
      : params.get("location") === "remote"
        ? "remote"
        : "all";
  const listOffset = Math.max(0, Number(params.get("librariesOffset")) || 0);
  const librarySearch = params.get("search") ?? "";
  const { libraries, libraryQuery, sources, jobs, activeJob, capabilities } =
    useKnowledgeWorkspaceQueries({
      ref,
      detail,
      documentsVisible: route.view === "raw",
      active,
      listOffset,
      librarySearch,
      sourceFilters: {
        query: route.sourceQuery,
        state: "all",
        sort: "newest",
      },
      location,
    });
  const library = libraryQuery.data
    ? {
        ...toLibrary(libraryQuery.data),
        sources: (sources.data?.items ?? []).map((source) =>
          toSource(source, ref.backend),
        ),
        sourceTotal: sources.data?.total,
      }
    : undefined;
  const busy = Boolean(
    activeJob && isKnowledgeJobBusy(activeJob.state ?? "unknown"),
  );
  const [openedSource, setOpenedSource] = useState<{
    scope: string;
    source: Source;
  }>();
  const selectedSource =
    openedSource?.scope === identity ? openedSource.source : undefined;
  const rows = library?.sources ?? [];
  const currentSource =
    rows.find((entry) => entry.id === selectedSource?.id) ?? selectedSource;
  const batch = rows.filter(isOrganizableSource).map((source) => source.id);
  const writes = useKnowledgeWrites(ref);
  const [modal, setModal] = useState<{
    kind:
      "create" | "remote-add" | "upload" | "organize" | "rebuild" | "settings";
    scope: string;
    batch?: string[];
    uploadContext?: KnowledgeUploadContext;
    uploadLoading?: boolean;
    uploadIntent?: string;
  }>();
  const [organizeIntents] = useState(
    () => new Map<string, KnowledgeOrganizeIntent>(),
  );
  const [createLoaded, setCreateLoaded] = useState(false);
  const [maintenance, setMaintenance] = useState<ReactNode>();
  const [wikiCommands] = useState<KnowledgeWikiCommands>(() => new Map());
  const [editorControllers] = useState(
    () => new Map<string, KnowledgeEditorController>(),
  );
  const [uploadQueues] = useState(
    () => new Map<string, KnowledgeUploadQueue>(),
  );
  const currentModal = modal?.scope === identity ? modal : undefined;
  useQueryEvents({
    topic: "knowledge",
    resource: detail
      ? {
          kind: "knowledge_library",
          id: route.libraryId,
          parentId: ref.connectionId,
        }
      : { kind: "global" },
    queryKeys: detail ? [root] : [knowledgeKeys.libraryLists],
    enabled: active,
    active,
  });
  const legacy =
    [
      ...(route.view === "wiki" ? [] : ["page"]),
      "pageSlug",
      "source",
      "pageSearch",
      "themeId",
      "pageType",
      "pagesOffset",
      "historyOffset",
    ].some((key) => params.has(key)) ||
    (params.has("view") &&
      ![
        detail ? "wiki" : "library",
        ...(detail ? ["raw", "graph"] : []),
      ].includes(params.get("view") ?? ""));
  useEffect(() => {
    if (!legacy) return;
    const query = new URLSearchParams(params);
    for (const key of [
      "view",
      "library",
      "page",
      "pageSlug",
      "source",
      "pageSearch",
      "themeId",
      "pageType",
      "pagesOffset",
      "historyOffset",
    ])
      query.delete(key);
    navigate(
      detail
        ? `/knowledge/${encodeURIComponent(route.libraryId)}?${query}`
        : "/knowledge",
      { replace: true },
    );
  }, [legacy, params, navigate, detail, route.libraryId]);
  function changeQuery(patch: Record<string, string>) {
    const query = new URLSearchParams(params);
    for (const [key, value] of Object.entries(patch)) query.set(key, value);
    navigate(`?${query}`);
  }
  function go(patch: Partial<Route>) {
    changeQuery(
      Object.fromEntries(
        Object.entries(patch).filter(
          (entry): entry is [string, string] => typeof entry[1] === "string",
        ),
      ),
    );
  }
  async function openAction(
    capability: string,
    callback: () => void | Promise<void>,
  ) {
    try {
      if (!knowledgeAdapter(ref).localConfiguration)
        await knowledgeAdapter(ref).assertCapability(capability);
      await callback();
    } catch (cause) {
      reportKnowledgeError(
        cause,
        `open-action:${identity}:${capability}`,
        t("knowledgeOperationFailed"),
      );
    }
  }
  async function openUpload() {
    if (!uploadQueues.has(identity))
      uploadQueues.set(identity, createKnowledgeUploadQueue());
    const uploadIntent = crypto.randomUUID();
    setModal({
      kind: "upload",
      scope: identity,
      uploadLoading: !adapter.localConfiguration,
      uploadIntent,
    });
    if (adapter.localConfiguration) return;
    try {
      const uploadContext = await adapter.getUploadContext();
      setModal((current) =>
        current?.uploadIntent === uploadIntent
          ? { ...current, uploadContext, uploadLoading: false }
          : current,
      );
    } catch (uploadError) {
      reportKnowledgeError(
        uploadError,
        `open-upload:${identity}`,
        t("knowledgeOperationFailed"),
      );
      setModal((current) =>
        current?.uploadIntent === uploadIntent ? undefined : current,
      );
    }
  }
  const retainedOrganize = organizeIntents.get(identity)?.get();
  const recoveringOrganize = Boolean(
    retainedOrganize?.start || retainedOrganize?.remoteCommand,
  );
  function openOrganize() {
    if (!organizeIntents.has(identity))
      organizeIntents.set(identity, createKnowledgeOrganizeIntent());
    setModal({ kind: "organize", scope: identity, batch: [...batch] });
  }
  function changeView(view: "raw" | "wiki" | "graph", tab?: string) {
    const query = new URLSearchParams(params);
    query.set("view", view);
    if (view !== "wiki" || (tab && tab !== params.get("wikiTab")))
      query.delete("page");
    if (tab) query.set("wikiTab", tab);
    navigate(`?${query}`);
  }
  const libraryActions = (
    <KnowledgeLibraryActions
      library={library}
      onGraph={() => changeView(route.view === "graph" ? "wiki" : "graph")}
      graphActive={route.view === "graph"}
      graphDisabled={
        route.view !== "graph" &&
        adapter.supportsServiceDetection &&
        !hasKnowledgeCapability(capabilities.data, "graph")
      }
      uploadDisabled={
        adapter.supportsServiceDetection &&
        !canUploadKnowledge(capabilities.data)
      }
      settingsDisabled={
        adapter.supportsServiceDetection &&
        !hasKnowledgeCapability(capabilities.data, "settings")
      }
      onUpload={() => void openUpload()}
      onSettings={() =>
        void openAction("settings", () =>
          setModal({ kind: "settings", scope: identity }),
        )
      }
      organizeDisabled={
        !library ||
        (!recoveringOrganize &&
          adapter.supportsServiceDetection &&
          !hasKnowledgeCapability(capabilities.data, "organize")) ||
        (!recoveringOrganize &&
          (jobs.isLoading || Boolean(jobs.error) || sources.isPending))
      }
      onOrganize={() => {
        // 原冻结请求通过 Adapter 对账；当前能力和任务状态只约束新接纳。
        if (recoveringOrganize) openOrganize();
        else void openAction("organize", openOrganize);
      }}
    />
  );
  const documents = (
    <>
      {route.view === "graph" ? (
        <Suspense fallback={<KnowledgeGraphLoading />}>
          <KnowledgeGraphView
            key={identity}
            resource={ref}
            center={params.get("graphCenter") ?? ""}
            onCenter={(slug) => {
              const query = new URLSearchParams(params);
              if (slug) query.set("graphCenter", slug);
              else query.delete("graphCenter");
              navigate(`?${query}`);
            }}
          />
        </Suspense>
      ) : (
        <Suspense fallback={<KnowledgeWikiLoading />}>
          <KnowledgeWikiView
            key={identity}
            resource={ref}
            view={route.view === "raw" ? "raw" : "wiki"}
            directoryTab={
              route.view === "raw"
                ? "raw"
                : params.get("wikiTab") === "summaries"
                  ? "summaries"
                  : "knowledge"
            }
            onDirectoryTab={(tab) =>
              changeView(
                tab === "raw" ? "raw" : "wiki",
                tab === "raw" ? undefined : tab,
              )
            }
            sourceCount={library?.sourceTotal ?? sources.data?.total ?? 0}
            sourceQuery={route.sourceQuery}
            onSourceQuery={(sourceQuery) => go({ sourceQuery })}
            sourceDirectory={
              route.view === "raw" ? (
                <Suspense fallback={<KnowledgeDocumentsLoading />}>
                  <RawView
                    rows={rows}
                    selectedId={currentSource?.id}
                    onOpen={(source) =>
                      setOpenedSource({ scope: identity, source })
                    }
                    route={route}
                    readState={{
                      ...reading(sources),
                      pending: libraryQuery.isPending || sources.isPending,
                      error: libraryQuery.error || sources.error,
                      retry: () => {
                        void libraryQuery.refetch();
                        void sources.refetch();
                      },
                    }}
                    hasMore={sources.hasNextPage}
                    pending={sources.isFetchingNextPage}
                    failed={sources.isFetchNextPageError}
                    onLoadMore={() => {
                      void sources.fetchNextPage({ cancelRefetch: false });
                    }}
                  />
                </Suspense>
              ) : undefined
            }
            content={
              route.view === "raw" ? (
                <Suspense fallback={<KnowledgeWikiLoading />}>
                  {library && (
                    <KnowledgeDocumentReader
                      library={library}
                      source={currentSource}
                      disabled={sources.isFetching}
                      deletionDisabled={busy}
                      onClose={() => setOpenedSource(undefined)}
                      onDeleted={() => setOpenedSource(undefined)}
                      onRebuild={(source) => {
                        const chosen = [source.id];
                        const key = `${identity}:rebuild:${JSON.stringify(chosen)}`;
                        if (!organizeIntents.has(key))
                          organizeIntents.set(
                            key,
                            createKnowledgeOrganizeIntent(),
                          );
                        setModal({
                          kind: "rebuild",
                          scope: identity,
                          batch: chosen,
                        });
                      }}
                    />
                  )}
                </Suspense>
              ) : undefined
            }
            commands={wikiCommands}
            onMaintenance={(page) =>
              void openAction("maintenanceChat", async () => {
                const module =
                  await import("./knowledge-wiki-maintenance-chat");
                setMaintenance(module.renderWikiMaintenance(ref, page));
              })
            }
            pageId={params.get("page") ?? ""}
            onPage={(id, tab) => {
              const query = new URLSearchParams(params);
              if (id) query.set("page", id);
              else query.delete("page");
              query.set("view", "wiki");
              if (tab) query.set("wikiTab", tab);
              navigate(`?${query}`);
            }}
            writable={
              Boolean(library) &&
              (!adapter.supportsServiceDetection ||
                hasKnowledgeCapability(capabilities.data, "write"))
            }
            maintenanceDisabled={
              adapter.supportsServiceDetection &&
              !hasKnowledgeCapability(capabilities.data, "maintenanceChat")
            }
            structureVersion={libraryQuery.data?.structureVersion ?? 0}
          />
        </Suspense>
      )}
    </>
  );
  return (
    <>
      {maintenance}
      {detail ? (
        <ResourcePageSurface
          contentLayout="pane"
          contentClassName="flex min-h-0 flex-col gap-5"
        >
          <PageHeader
            title={library?.name || params.get("knowledge") || t("knowledge")}
            layout="detail"
            className="items-center"
            actions={libraryActions}
          />
          <Separator />
          {documents}
        </ResourcePageSurface>
      ) : (
        <>
          <LibraryList
            libraries={(libraries.data?.items ?? []).map(toLibrary)}
            state={
              libraries.error
                ? "error"
                : libraries.isPending
                  ? "loading"
                  : undefined
            }
            error={libraries.error}
            onRetry={() => void libraries.refetch()}
            search={librarySearch}
            onSearchChange={(search) =>
              changeQuery({ search, librariesOffset: "0" })
            }
            location={location}
            onLocationChange={(value) =>
              changeQuery({ location: value, librariesOffset: "0" })
            }
            total={libraries.data?.total}
            open={(value) =>
              navigate(
                `/knowledge/${encodeURIComponent(value.id)}?${new URLSearchParams({ backend: value.ref.backend, connection: value.ref.connectionId, title: value.name })}`,
              )
            }
            create={() => {
              if (!editorControllers.get("create")?.getSnapshot().command)
                editorControllers.set(
                  "create",
                  createKnowledgeEditorController(),
                );
              setCreateLoaded(true);
              setModal({ kind: "create", scope: identity });
            }}
            canCreate
            onAddRemote={() =>
              setModal({ kind: "remote-add", scope: identity })
            }
            onDelete={async (value, requestId) => {
              await deleteLibrary(value, requestId);
              await client.invalidateQueries({
                queryKey: knowledgeKeys.libraryLists,
              });
            }}
          />
          {(libraries.data?.total ?? 0) > 50 && (
            <Pagination
              labels={paginationLabels}
              page={Math.floor(listOffset / 50) + 1}
              pageSize={50}
              totalItems={libraries.data?.total ?? 0}
              onPageChange={(page) =>
                changeQuery({ librariesOffset: String((page - 1) * 50) })
              }
            />
          )}
        </>
      )}
      {currentModal?.kind === "remote-add" && (
        <Suspense fallback={null}>
          <KnowledgeRemoteAddDialog
            onClose={() => setModal(undefined)}
            onAdded={async (receipt) => {
              const id = receipt.mount?.knowledgeBaseId;
              const connection = receipt.mount?.connectionId;
              if (!id || !connection) throw new Error(t("invalidResource"));
              await client.invalidateQueries({
                queryKey: knowledgeKeys.libraryLists,
              });
              setModal(undefined);
              navigate(
                `/knowledge/${encodeURIComponent(id)}?${new URLSearchParams({ backend: receipt.connection?.backend ?? "integration", connection, title: receipt.connection?.detection?.library?.name ?? id })}`,
              );
            }}
          />
        </Suspense>
      )}
      {createLoaded && (
        <CreateLibrary
          opened={currentModal?.kind === "create"}
          controller={editorControllers.get("create")}
          close={() => {
            if (!editorControllers.get("create")?.getSnapshot().command)
              editorControllers.delete("create");
            setModal(undefined);
          }}
          create={async (value, execution, requestId, controller) => {
            const { buildCreateLibraryBody } =
              await import("./knowledge-library-write-body");
            const response = await knowledgeAdapter(value.ref).createLibrary({
              body: {
                ...buildCreateLibraryBody(requestId, value),
                ...(knowledgeAdapter(value.ref).localConfiguration
                  ? { executionDefaults: buildExecutionDefaults(execution) }
                  : {}),
                ...(knowledgeAdapter(value.ref).localConfiguration &&
                controller.getSnapshot().draft.embedding.baseUrl
                  ? {
                      embeddingProfile: {
                        baseUrl:
                          controller.getSnapshot().draft.embedding.baseUrl,
                        model: controller.getSnapshot().draft.embedding.model,
                        apiKey: controller.getSnapshot().draft.embedding.apiKey,
                      },
                    }
                  : {}),
              },
              throwOnError: true,
            });
            const id =
              response.data.resourceId ?? response.data.libraryId ?? "";
            if (!id) throw new Error(t("createLibraryFailed"));
            controller.acknowledgeLibrary({
              ...value,
              id,
              revision: response.data.version ?? value.revision,
              ref: { ...value.ref, knowledgeBaseId: id },
            });
            if (knowledgeAdapter(value.ref).localConfiguration)
              controller.acknowledgeExecution(execution, 1);
            const embedding = controller.getSnapshot().draft.embedding;
            if (embedding.baseUrl)
              controller.acknowledgeEmbedding(
                { ...embedding, hasApiKey: true },
                1,
                response.data.version ?? value.revision,
                controller.getSnapshot().command?.embeddingRequestId ?? "",
              );
            await client.invalidateQueries({
              queryKey: knowledgeKeys.libraryLists,
            });
            setModal(undefined);
            navigate(
              `/knowledge/${encodeURIComponent(id)}?${new URLSearchParams({ title: value.name, ...(value.ref.backend !== "local" ? { backend: value.ref.backend, connection: value.ref.connectionId } : {}) })}`,
            );
          }}
        />
      )}
      {library && currentModal?.kind === "settings" && (
        <KnowledgeLibrarySettings
          library={library}
          controllers={editorControllers}
          close={() => setModal(undefined)}
        />
      )}
      {library && currentModal?.kind === "upload" && (
        <UploadSources
          queue={uploadQueues.get(identity)}
          mode={currentModal.uploadContext?.uploadMode}
          pending={currentModal.uploadLoading}
          confirmationVersion={currentModal.uploadContext?.connectionRevision}
          onRedetect={
            adapter.supportsServiceDetection
              ? async () => {
                  const latest = await adapter.getCapabilities();
                  const revision = latest.connectionRevision;
                  if (!revision) throw new Error(t("invalidResource"));
                  await adapter.redetectService(revision, crypto.randomUUID());
                  await client.invalidateQueries({
                    queryKey: knowledgeKeys.resource(ref),
                  });
                  const uploadContext = await adapter.getUploadContext();
                  setModal({
                    ...currentModal,
                    uploadContext,
                    uploadLoading: false,
                  });
                }
              : undefined
          }
          opened
          close={() => setModal(undefined)}
          upload={async (file, requestId, signal) => {
            const { uploadKnowledgeSource } =
              await import("./knowledge-upload-source");
            const receipt = await uploadKnowledgeSource(
              ref,
              file,
              requestId,
              signal,
              currentModal.uploadContext,
            );
            await Promise.all([
              client.invalidateQueries({ queryKey: [...root, "sources"] }),
              client.invalidateQueries({ queryKey: [...root, "detail"] }),
            ]);
            return receipt;
          }}
        />
      )}

      {library &&
        (currentModal?.kind === "organize" ||
          currentModal?.kind === "rebuild") && (
          <KnowledgeOrganizeDialog
            library={library}
            rebuild={currentModal.kind === "rebuild"}
            batch={currentModal.batch ?? []}
            operation={writes.organize}
            intent={organizeIntents.get(
              currentModal.kind === "rebuild"
                ? `${identity}:rebuild:${JSON.stringify(currentModal.batch ?? [])}`
                : identity,
            )}
            onClose={() => {
              const key =
                currentModal.kind === "rebuild"
                  ? `${identity}:rebuild:${JSON.stringify(currentModal.batch ?? [])}`
                  : identity;
              // 普通取消舍弃草稿；已提交请求保留原范围与要求，供待定结果核验。
              if (
                !organizeIntents.get(key)?.get().start &&
                !organizeIntents.get(key)?.get().remoteCommand
              )
                organizeIntents.delete(key);
              setModal(undefined);
            }}
            onAccepted={() => {
              organizeIntents.delete(
                currentModal.kind === "rebuild"
                  ? `${identity}:rebuild:${JSON.stringify(currentModal.batch ?? [])}`
                  : identity,
              );
              setModal((current) =>
                (current?.kind === "organize" || current?.kind === "rebuild") &&
                current.kind === currentModal.kind &&
                current.scope === identity &&
                JSON.stringify(current.batch ?? []) ===
                  JSON.stringify(currentModal.batch ?? [])
                  ? undefined
                  : current,
              );
            }}
          />
        )}
    </>
  );
}
