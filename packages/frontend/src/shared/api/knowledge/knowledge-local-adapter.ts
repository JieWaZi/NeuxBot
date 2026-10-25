import * as api from "@/shared/api/generated/sdk.gen";
import { knowledgeAdapterMetadata } from "./knowledge-adapter-metadata";
import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import type { ResourceRef } from "./knowledge-adapter-types";
import {
  scopedKnowledgeOptions,
  knowledgeUnsupported,
  assertKnowledgeCapability,
  type KnowledgeOptions,
  type KnowledgeUploadContext,
  type KnowledgeCapabilities,
  type KnowledgePageReadOptions,
  type KnowledgeRevisionsOptions,
  type KnowledgeOrganizeOptions,
} from "./knowledge-adapter-types";

/** 本机实现只拥有本机 HTTP 目标，业务持久状态仍由原 API 管理。 */
export function localKnowledgeAdapter(ref: ResourceRef) {
  const scope = <T extends { path?: object }>(options: T) =>
    scopedKnowledgeOptions(ref, options);
  const getCapabilities = async (
    _signal?: AbortSignal,
  ): Promise<KnowledgeCapabilities> => {
    _signal?.throwIfAborted();
    const keys = [
      "read",
      "write",
      "rawOnly",
      "organize",
      "rebuild",
      "sourcePurge",
      "libraryDelete",
      "themeWrite",
      "themes",
      "sourceRead",
      "sourceDownload",
      "sourceContent",
      "history",
      "settings",
      "localConfiguration",
      "activity",
      "graph",
      "maintenanceChat",
    ];
    return {
      operations: Object.fromEntries(keys.map((key) => [key, true])),
      details: Object.fromEntries(
        keys.map((key) => [
          key,
          { supported: true, permission: "allowed", condition: "ready" },
        ]),
      ),
      profiles: [],
    };
  };
  return {
    ...knowledgeAdapterMetadata(ref),
    listConnections: () => api.listConnections<true>({ throwOnError: true }),
    getCapabilities,
    assertCapability: async (key: string, signal?: AbortSignal) => {
      signal?.throwIfAborted();
      assertKnowledgeCapability(await getCapabilities(signal), key);
    },
    listAvailableLibraries: (
      options?: KnowledgeOptions<typeof api.listAvailableLibraries>,
    ) =>
      api.listAvailableLibraries<true>({
        ...options,
        throwOnError: true as const,
      }),
    testLibraryEmbedding: (
      options: KnowledgeOptions<typeof api.testLibraryEmbedding>,
    ) =>
      api.testLibraryEmbedding<true>({
        ...options,
        body: {
          ...options.body,
          libraryId: ref.knowledgeBaseId || undefined,
        },
        throwOnError: true,
      }),
    getLibrary: (options: KnowledgeOptions<typeof api.getLibrary>) =>
      api.getLibrary<true>(scope(options)),
    createLibrary: (options: KnowledgeOptions<typeof api.createLibrary>) =>
      api.createLibrary<true>(scope(options)),
    updateLibrary: (options: KnowledgeOptions<typeof api.updateLibrary>) =>
      api.updateLibrary<true>(scope(options)),
    removeMount: async () => knowledgeUnsupported(),
    deleteLibrary: (options: KnowledgeOptions<typeof api.deleteLibrary>) =>
      api.deleteLibrary<true>(scope(options)),
    getLibraryDeletion: (
      options: KnowledgeOptions<typeof api.getLibraryDeletion>,
    ) => api.getLibraryDeletion<true>(scope(options)),
    recoverLibraryDeletion: (
      options: KnowledgeOptions<typeof api.recoverLibraryDeletion>,
    ) => api.recoverLibraryDeletion<true>(scope(options)),
    listSources: (options: KnowledgeOptions<typeof api.listSources>) =>
      api.listSources<true>(scope(options)),
    getSource: (options: KnowledgeOptions<typeof api.getSource>) =>
      api.getSource<true>(scope(options)),
    redetectService: async () => knowledgeUnsupported(),
    getUploadContext: async (
      _signal?: AbortSignal,
    ): Promise<KnowledgeUploadContext | undefined> => {
      _signal?.throwIfAborted();
      return undefined;
    },
    uploadSource: (options: KnowledgeOptions<typeof api.importSource>) =>
      api.importSource<true>(scope(options)),
    importSource: (options: KnowledgeOptions<typeof api.importSource>) =>
      api.importSource<true>(scope(options)),
    archiveSource: (options: KnowledgeOptions<typeof api.archiveSource>) =>
      api.archiveSource<true>(scope(options)),
    getRaw: (options: KnowledgeOptions<typeof api.getRaw>) =>
      api.getRaw<true>(scope(options)),
    getSourceChunks: (options: KnowledgeOptions<typeof api.getSourceChunks>) =>
      api.getSourceChunks<true>(scope(options)),
    previewSourcePurge: (
      options: KnowledgeOptions<typeof api.previewSourcePurge>,
    ) => api.previewSourcePurge<true>(scope(options)),
    confirmSourcePurge: (
      options: KnowledgeOptions<typeof api.confirmSourcePurge>,
    ) => api.confirmSourcePurge<true>(scope(options)),
    listSourcePurges: (
      options: KnowledgeOptions<typeof api.listSourcePurges>,
    ) => api.listSourcePurges<true>(scope(options)),
    getSourcePurge: (options: KnowledgeOptions<typeof api.getSourcePurge>) =>
      api.getSourcePurge<true>(scope(options)),
    recoverSourcePurge: (
      options: KnowledgeOptions<typeof api.recoverSourcePurge>,
    ) => api.recoverSourcePurge<true>(scope(options)),
    listJobs: (options: KnowledgeOptions<typeof api.listJobs>) =>
      api.listJobs<true>(scope(options)),
    getJob: (options: KnowledgeOptions<typeof api.getJob>) =>
      api.getJob<true>(scope(options)),
    resumeJob: (options: KnowledgeOptions<typeof api.resumeJob>) =>
      api.resumeJob<true>(scope(options)),
    retryJob: (options: KnowledgeOptions<typeof api.retryJob>) =>
      api.retryJob<true>(scope(options)),
    sendMaintenanceMessage: (
      options: KnowledgeOptions<typeof api.sendMaintenanceMessage>,
    ) => api.sendMaintenanceMessage<true>(scope(options)),
    listPageDirectory: (
      options: KnowledgeOptions<typeof api.listPageDirectory>,
    ) => api.listPageDirectory<true>(scope(options)),
    listPages: (options: KnowledgeOptions<typeof api.listPages>) =>
      api.listPages<true>(scope(options)),
    getPageBySlug: (options: KnowledgeOptions<typeof api.getPageBySlug>) =>
      api.getPageBySlug<true>(scope(options)),
    getWikiIndex: (options: KnowledgeOptions<typeof api.getWikiIndex>) =>
      api.getWikiIndex<true>(scope(options)),
    listThemes: (options: KnowledgeOptions<typeof api.listThemes>) =>
      api.listThemes<true>(scope(options)),
    createTheme: (options: KnowledgeOptions<typeof api.createTheme>) =>
      api.createTheme<true>(scope(options)),
    writeTheme: (options: KnowledgeOptions<typeof api.writeTheme>) =>
      api.writeTheme<true>(scope(options)),
    createPage: (options: KnowledgeOptions<typeof api.createPage>) =>
      api.createPage<true>(scope(options)),
    writePage: (options: KnowledgeOptions<typeof api.writePage>) =>
      api.writePage<true>(scope(options)),
    restorePage: (options: KnowledgeOptions<typeof api.restorePage>) =>
      api.restorePage<true>(scope(options)),
    getLibraryExecutionDefaults: (
      options: KnowledgeOptions<typeof api.getLibraryExecutionDefaults>,
    ) => api.getLibraryExecutionDefaults<true>(scope(options)),
    updateLibraryExecutionDefaults: (
      options: KnowledgeOptions<typeof api.updateLibraryExecutionDefaults>,
    ) => api.updateLibraryExecutionDefaults<true>(scope(options)),
    getLibraryEmbeddingProfile: (
      options: KnowledgeOptions<typeof api.getLibraryEmbeddingProfile>,
    ) => api.getLibraryEmbeddingProfile<true>(scope(options)),
    updateLibraryEmbeddingProfile: (
      options: KnowledgeOptions<typeof api.updateLibraryEmbeddingProfile>,
    ) => api.updateLibraryEmbeddingProfile<true>(scope(options)),
    listLibraryActivity: (
      options: KnowledgeOptions<typeof api.listLibraryActivity>,
    ) => api.listLibraryActivity<true>(scope(options)),
    previewChunking: (options: KnowledgeOptions<typeof api.previewChunking>) =>
      api.previewChunking<true>(scope(options)),
    getPage: (options: KnowledgePageReadOptions) =>
      api.getPage<true>(scope({ ...options, query: undefined })),
    listRevisions: (options: KnowledgeRevisionsOptions) =>
      api.listRevisions<true>(scope(options)),
    getRevision: async (
      options: KnowledgeOptions<typeof api.getRemoteRevision>,
    ) => {
      let offset = 0;
      let total = Infinity;
      const seen = new Set<number>();
      while (offset < total) {
        const page = await api.listRevisions<true>(
          scope({ ...options, query: { offset, limit: 100 } }),
        );
        const next = page.data.items ?? [];
        total = page.data.total ?? offset + next.length;
        if (
          (!next.length && offset < total) ||
          next.some((row) => !row.version || seen.has(row.version))
        )
          throw new ApiError(
            i18n.t("knowledge:incompleteHistory", {
              defaultValue: knowledgeZh.incompleteHistory,
            }),
            "KnowledgeRemoteIncompatible",
            502,
          );
        for (const row of next) if (row.version) seen.add(row.version);
        const data = next.find((row) => row.version === options.path.version);
        if (data) return { data };
        offset += next.length;
      }
      throw new ApiError(
        i18n.t("knowledge:revisionNotFound", {
          defaultValue: knowledgeZh.revisionNotFound,
        }),
        "KnowledgeNotFound",
        404,
      );
    },
    organize: (options: KnowledgeOrganizeOptions) => {
      if ("profileId" in options.body)
        return Promise.reject(
          new ApiError(
            i18n.t("knowledge:invalidExecutionProfile", {
              defaultValue: knowledgeZh.invalidExecutionProfile,
            }),
            "KnowledgeInvalid",
            400,
          ),
        );
      return api.organize<true>(scope({ ...options, body: options.body }));
    },
    organizeRemote: async (
      _options: KnowledgeOptions<typeof api.organizeRemote>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getGraph: async (_options: KnowledgeOptions<typeof api.getRemoteGraph>) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getRemoteGraph: async (
      _options: KnowledgeOptions<typeof api.getRemoteGraph>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getOperation: async (
      _options: KnowledgeOptions<typeof api.getRemoteOperation>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    listOperations: async (
      _options: KnowledgeOptions<typeof api.listRemoteOperations>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
    getReceipt: async (
      _options: KnowledgeOptions<typeof api.getRemoteReceipt>,
    ) => {
      _options.signal?.throwIfAborted();
      return knowledgeUnsupported();
    },
  };
}
