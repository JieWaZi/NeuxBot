import * as api from "@/shared/api/generated/sdk.gen";
import { knowledgeAdapterMetadata } from "./knowledge-adapter-metadata";
import type { ListRemoteThemesResponse } from "@/shared/api/generated/types.gen";
import type { ResourceRef } from "./knowledge-adapter-types";
import {
  scopedKnowledgeOptions,
  knowledgeUnsupported,
  knowledgeDirectoryIncomplete,
  assertKnowledgeCapability,
  type KnowledgeOptions,
  type KnowledgeUploadContext,
  type KnowledgeCapabilities,
  type KnowledgePageReadOptions,
  type KnowledgeRevisionsOptions,
  type KnowledgeOrganizeOptions,
} from "./knowledge-adapter-types";

/** 远端实现只访问连接范围 HTTP，不持有任何本机能力回退。 */
export function remoteKnowledgeAdapter(ref: ResourceRef) {
  const official = ref.backend === "weknora";
  const scope = <T extends { path?: object }>(options: T) =>
    scopedKnowledgeOptions(ref, options);
  const getCapabilities = async (
    signal?: AbortSignal,
  ): Promise<KnowledgeCapabilities> => {
    signal?.throwIfAborted();
    if (official) {
      const keys = [
        "read",
        "themes",
        "sourceRead",
        "sourceDownload",
        "history",
        "graph",
      ];
      return {
        operations: Object.fromEntries(keys.map((key) => [key, true])),
        details: Object.fromEntries(
          keys.map((key) => [
            key,
            { supported: true, permission: "unknown", condition: "ready" },
          ]),
        ),
        profiles: [],
      };
    }
    const { getRemoteKnowledgeCapabilities } =
      await import("./knowledge-remote-operations");
    return getRemoteKnowledgeCapabilities(ref, signal);
  };
  const assertCapability = async (key: string, signal?: AbortSignal) => {
    signal?.throwIfAborted();
    if (official) {
      if ((await getCapabilities(signal)).details[key]?.supported) return;
      knowledgeUnsupported();
    }
    assertKnowledgeCapability(await getCapabilities(signal), key);
  };
  const assertWriteCapability = async (
    key: string,
    requestId: string,
    operation: string,
    signal?: AbortSignal,
  ) => {
    if (official) return assertCapability(key, signal);
    const { assertRemoteKnowledgeWrite } =
      await import("./knowledge-remote-operations");
    return assertRemoteKnowledgeWrite(ref, key, requestId, operation, signal);
  };
  const organizeRemote = async (
    options: KnowledgeOptions<typeof api.organizeRemote>,
  ) => {
    await assertWriteCapability(
      "rebuild" in options.body && options.body.rebuild
        ? "rebuild"
        : "organize",
      options.body.requestId,
      "job.accept",
      options.signal ?? undefined,
    );
    return api.organizeRemote<true>(
      scope({
        ...options,
        body: {
          requestId: options.body.requestId,
          expectedVersion: options.body.expectedVersion,
          structureVersion: options.body.structureVersion,
          sourceVersionIds: options.body.sourceVersionIds,
          profileId: options.body.profileId,
          ...("rebuild" in options.body
            ? { rebuild: options.body.rebuild }
            : {}),
          ...("instruction" in options.body
            ? { instruction: options.body.instruction }
            : {}),
        },
      }),
    );
  };
  return {
    ...knowledgeAdapterMetadata(ref),
    listConnections: () => api.listConnections<true>({ throwOnError: true }),
    getCapabilities,
    assertCapability,
    listAvailableLibraries: (
      options?: KnowledgeOptions<typeof api.listAvailableLibraries>,
    ) =>
      api.listAvailableLibraries<true>({
        ...options,
        throwOnError: true as const,
      }),
    getLibrary: (options: KnowledgeOptions<typeof api.getLibrary>) =>
      api.getRemoteLibrary<true>(scope(options)),
    getLibraryDeletion: (
      options: KnowledgeOptions<typeof api.getLibraryDeletion>,
    ) => api.getRemoteLibraryDeletion<true>(scope(options)),
    listSources: (options: KnowledgeOptions<typeof api.listSources>) =>
      api.listRemoteSources<true>(scope(options)),
    getSource: (options: KnowledgeOptions<typeof api.getSource>) =>
      api.getRemoteSource<true>(scope(options)),
    getRaw: (options: KnowledgeOptions<typeof api.getRaw>) =>
      api.getRemoteRaw<true>(scope(options)),
    getSourcePurge: (options: KnowledgeOptions<typeof api.getSourcePurge>) =>
      official
        ? Promise.resolve().then(knowledgeUnsupported)
        : api.getRemoteSourcePurge<true>(scope(options)),
    listSourcePurges: (
      options: KnowledgeOptions<typeof api.listSourcePurges>,
    ) =>
      official
        ? Promise.resolve().then(knowledgeUnsupported)
        : api.listRemoteSourcePurges<true>(scope(options)),
    previewSourcePurge: (
      options: KnowledgeOptions<typeof api.previewSourcePurge>,
    ) =>
      official
        ? Promise.resolve().then(knowledgeUnsupported)
        : api.previewRemoteSourcePurge<true>(scope(options)),
    getJob: (options: KnowledgeOptions<typeof api.getJob>) =>
      official
        ? Promise.resolve().then(knowledgeUnsupported)
        : api.getRemoteJob<true>(scope(options)),
    sendMaintenanceMessage: async (
      _options: KnowledgeOptions<typeof api.sendMaintenanceMessage>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    listPageDirectory: (
      options: KnowledgeOptions<typeof api.listPageDirectory>,
    ) => api.listRemotePageDirectory<true>(scope(options)),
    listPages: (options: KnowledgeOptions<typeof api.listPages>) =>
      api.listRemotePages<true>(scope(options)),
    getPageBySlug: (options: KnowledgeOptions<typeof api.getPageBySlug>) =>
      api.getRemotePageBySlug<true>(scope(options)),
    updateLibrary: async (
      options: KnowledgeOptions<typeof api.updateLibrary>,
    ) => {
      await assertWriteCapability(
        "settings",
        options.body.requestId,
        "library.update",
        options.signal ?? undefined,
      );
      return api.updateRemoteLibrary<true>(scope(options));
    },
    removeMount: async (
      options: KnowledgeOptions<typeof api.removeRemoteMount>,
    ) => {
      if (ref.backend !== "integration") knowledgeUnsupported();
      return api.removeRemoteMount<true>({
        ...options,
        throwOnError: true,
      });
    },
    deleteLibrary: async (
      options: KnowledgeOptions<typeof api.deleteLibrary>,
    ) => {
      await assertWriteCapability(
        "libraryDelete",
        options.body.requestId,
        "library.delete",
        options.signal ?? undefined,
      );
      return api.deleteRemoteLibrary<true>(scope(options));
    },
    recoverLibraryDeletion: async (
      options: KnowledgeOptions<typeof api.recoverLibraryDeletion>,
    ) => {
      options.signal?.throwIfAborted();
      if (official) knowledgeUnsupported();
      const { recoverRemoteKnowledgeDeletion } =
        await import("./knowledge-remote-operations");
      return recoverRemoteKnowledgeDeletion(ref, options);
    },
    redetectService: async (expectedRevision: number, requestId: string) => {
      if (ref.backend !== "integration") knowledgeUnsupported();
      const result = await api.redetectConnection<true>({
        path: { connectionId: ref.connectionId },
        body: { expectedRevision, requestId },
        throwOnError: true,
      });
      return result.data;
    },
    getUploadContext: async (
      signal?: AbortSignal,
    ): Promise<KnowledgeUploadContext | undefined> => {
      if (ref.backend !== "integration") {
        await assertCapability("rawOnly", signal);
        return undefined;
      }
      const { remoteUploadContext } = await import("./knowledge-remote-upload");
      return remoteUploadContext(await getCapabilities(signal));
    },
    uploadSource: async (
      options: KnowledgeOptions<typeof api.importSource>,
      confirmed?: KnowledgeUploadContext,
    ) => {
      if (ref.backend === "integration") {
        const { uploadRemoteKnowledgeSource } =
          await import("./knowledge-remote-upload");
        return uploadRemoteKnowledgeSource(ref, options, confirmed);
      }
      await assertWriteCapability(
        "rawOnly",
        options.body.requestId,
        "source.import",
        options.signal ?? undefined,
      );
      return api.importRemoteSource<true>(scope(options));
    },
    importSource: async (
      options: KnowledgeOptions<typeof api.importSource>,
    ) => {
      await assertWriteCapability(
        "rawOnly",
        options.body.requestId,
        "source.import",
        options.signal ?? undefined,
      );
      return api.importRemoteSource<true>(scope(options));
    },
    archiveSource: async (
      options: KnowledgeOptions<typeof api.archiveSource>,
    ) => {
      await assertWriteCapability(
        "write",
        options.body.requestId,
        "source.archive",
        options.signal ?? undefined,
      );
      return api.archiveRemoteSource<true>(scope(options));
    },
    confirmSourcePurge: async (
      options: KnowledgeOptions<typeof api.confirmSourcePurge>,
    ) => {
      await assertWriteCapability(
        "sourcePurge",
        options.body.requestId,
        "source.purge.confirm",
        options.signal ?? undefined,
      );
      return api.confirmRemoteSourcePurge<true>(scope(options));
    },
    recoverSourcePurge: async (
      options: KnowledgeOptions<typeof api.recoverSourcePurge>,
    ) => {
      await assertWriteCapability(
        "sourcePurge",
        options.body.requestId,
        "source.purge.recover",
        options.signal ?? undefined,
      );
      return api.recoverRemoteSourcePurge<true>(scope(options));
    },
    createTheme: async (options: KnowledgeOptions<typeof api.createTheme>) => {
      await assertWriteCapability(
        "themeWrite",
        options.body.requestId,
        "theme.create",
        options.signal ?? undefined,
      );
      return api.createRemoteTheme<true>(scope(options));
    },
    writeTheme: async (options: KnowledgeOptions<typeof api.writeTheme>) => {
      await assertWriteCapability(
        "themeWrite",
        options.body.requestId,
        "theme.write",
        options.signal ?? undefined,
      );
      return api.writeRemoteTheme<true>(scope(options));
    },
    createPage: async (options: KnowledgeOptions<typeof api.createPage>) => {
      await assertWriteCapability(
        "write",
        options.body.requestId,
        "page.create",
        options.signal ?? undefined,
      );
      return api.createRemotePage<true>(scope(options));
    },
    writePage: async (options: KnowledgeOptions<typeof api.writePage>) => {
      await assertWriteCapability(
        "write",
        options.body.requestId,
        "page.write",
        options.signal ?? undefined,
      );
      return api.writeRemotePage<true>(scope(options));
    },
    restorePage: async (options: KnowledgeOptions<typeof api.restorePage>) => {
      await assertWriteCapability(
        "write",
        options.body.requestId,
        "page.restore",
        options.signal ?? undefined,
      );
      return api.restoreRemotePage<true>(scope(options));
    },
    getLibraryExecutionDefaults: async (
      _options: KnowledgeOptions<typeof api.getLibraryExecutionDefaults>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    updateLibraryExecutionDefaults: async (
      _options: KnowledgeOptions<typeof api.updateLibraryExecutionDefaults>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getLibraryEmbeddingProfile: async (
      _options: KnowledgeOptions<typeof api.getLibraryEmbeddingProfile>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    updateLibraryEmbeddingProfile: async (
      _options: KnowledgeOptions<typeof api.updateLibraryEmbeddingProfile>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    testLibraryEmbedding: async (
      _options: KnowledgeOptions<typeof api.testLibraryEmbedding>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getWikiIndex: async (
      _options: KnowledgeOptions<typeof api.getWikiIndex>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    previewChunking: async (
      _options: KnowledgeOptions<typeof api.previewChunking>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    resumeJob: async (_options: KnowledgeOptions<typeof api.resumeJob>) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    retryJob: async (_options: KnowledgeOptions<typeof api.retryJob>) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    createLibrary: async (
      options: KnowledgeOptions<typeof api.createLibrary>,
    ) => {
      options.signal?.throwIfAborted();
      if (official) knowledgeUnsupported();
      const { executionDefaults, embeddingProfile, ...body } = options.body;
      if (executionDefaults || embeddingProfile) knowledgeUnsupported();
      // 创建属于连接级授权，不能借用已有库的 write 权限或读取空库能力。
      return api.createRemoteLibrary<true>({
        ...options,
        path: { connectionId: ref.connectionId },
        body,
        throwOnError: true,
      });
    },
    getSourceChunks: async (
      options: KnowledgeOptions<typeof api.getSourceChunks>,
    ) => {
      if (official) knowledgeUnsupported();
      return api.getRemoteSourceChunks<true>(scope(options));
    },
    listLibraryActivity: async (
      options: KnowledgeOptions<typeof api.listLibraryActivity>,
    ) => {
      if (official) knowledgeUnsupported();
      return api.listRemoteLibraryActivity<true>(scope(options));
    },
    listJobs: async (options: KnowledgeOptions<typeof api.listJobs>) => {
      if (official) knowledgeUnsupported();
      const result = await api.listRemoteJobs<true>(scope(options));
      return { ...result, data: { ...result.data, activeJob: undefined } };
    },
    getPage: (options: KnowledgePageReadOptions) =>
      api.getRemotePage<true>(scope(options)),
    listRevisions: (options: KnowledgeRevisionsOptions) =>
      api.listRemoteRevisions<true>(scope(options)),
    getRevision: (options: KnowledgeOptions<typeof api.getRemoteRevision>) =>
      api.getRemoteRevision<true>(scope(options)),
    listThemes: async (options: KnowledgeOptions<typeof api.listThemes>) => {
      const signal = options.signal;
      const items: NonNullable<ListRemoteThemesResponse["items"]> = [];
      const seen = new Set<string>();
      const parents = [""];
      while (parents.length) {
        signal?.throwIfAborted();
        const parentId = parents.shift() ?? "";
        const { data } = await api.listRemoteThemes<true>(
          scope({ ...options, query: { parentId, all: parentId === "" } }),
        );
        signal?.throwIfAborted();
        if (data.complete) {
          const rows = data.items ?? [];
          const ids = new Set<string>();
          for (const row of rows) {
            if (!row.id || ids.has(row.id)) knowledgeDirectoryIncomplete();
            ids.add(row.id);
          }
          for (const row of rows)
            if (row.parentId && !ids.has(row.parentId))
              knowledgeDirectoryIncomplete();
          return { data: { items: rows } };
        }
        for (const row of data.items ?? []) {
          if (!row.id || seen.has(row.id) || (row.parentId ?? "") !== parentId)
            knowledgeDirectoryIncomplete();
          seen.add(row.id);
          items.push(row);
          if (row.hasChildren) parents.push(row.id);
        }
      }
      return { data: { items } };
    },
    organize: async (options: KnowledgeOrganizeOptions) => {
      if (!("profileId" in options.body)) knowledgeUnsupported();
      const result = await organizeRemote(
        scope({ ...options, body: options.body }),
      );
      return { ...result, data: { receipt: result.data } };
    },
    organizeRemote,
    getGraph: async (options: KnowledgeOptions<typeof api.getRemoteGraph>) => {
      if (!official && ref.backend !== "integration") knowledgeUnsupported();
      return api.getRemoteGraph<true>(scope(options));
    },
    getRemoteGraph: async (
      options: KnowledgeOptions<typeof api.getRemoteGraph>,
    ) => {
      if (!official && ref.backend !== "integration") knowledgeUnsupported();
      return api.getRemoteGraph<true>(scope(options));
    },
    getOperation: (options: KnowledgeOptions<typeof api.getRemoteOperation>) =>
      api.getRemoteOperation<true>(scope(options)),
    listOperations: (
      options: KnowledgeOptions<typeof api.listRemoteOperations>,
    ) => api.listRemoteOperations<true>(scope(options)),
    getReceipt: async (
      options: KnowledgeOptions<typeof api.getRemoteReceipt>,
    ) => {
      if (official) knowledgeUnsupported();
      return api.getRemoteReceipt<true>(scope(options));
    },
  };
}
