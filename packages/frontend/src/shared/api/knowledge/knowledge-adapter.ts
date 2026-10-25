import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import type { knowledgeAdapter as createImplementation } from "./knowledge-adapter-implementation";
import { knowledgeAdapterMetadata } from "./knowledge-adapter-metadata";
import type { ResourceRef } from "./knowledge-adapter-types";
export type { KnowledgeCapabilities } from "./knowledge-adapter-types";

type Implementation = ReturnType<typeof createImplementation>;
type MethodName = {
  [Key in keyof Implementation]: Implementation[Key] extends (
    ...args: never[]
  ) => unknown
    ? Key
    : never;
}[keyof Implementation];
type Methods = {
  [Key in MethodName]: (
    ...args: Parameters<Implementation[Key]>
  ) => Promise<Awaited<ReturnType<Implementation[Key]>>>;
};

/** 同步固定身份与能力策略，协议实现在首次操作时加载；页面外壳无需等待。 */
export function knowledgeAdapter(input: ResourceRef): Implementation {
  const ref = Object.freeze({ ...input });
  if (
    ref.backend !== "local" &&
    !(
      (ref.backend === "custom" ||
        ref.backend === "weknora" ||
        ref.backend === "integration") &&
      ref.connectionId &&
      ref.connectionId !== "local"
    )
  )
    throw new ApiError(
      i18n.t("knowledge:invalidResource", {
        defaultValue: knowledgeZh.invalidResource,
      }),
      "KnowledgeInvalid",
      400,
    );
  let loaded: Promise<Implementation> | undefined;
  const load = () =>
    (loaded ??= import("./knowledge-adapter-implementation")
      .then((module) => module.knowledgeAdapter(ref))
      .catch((cause: unknown) => {
        loaded = undefined;
        throw cause;
      }));
  const call = <Key extends MethodName>(key: Key): Methods[Key] => {
    const invoke = async (
      ...args: Parameters<Implementation[Key]>
    ): Promise<Awaited<ReturnType<Implementation[Key]>>> => {
      const implementation = await load();
      const method = implementation[key] as (
        ...values: Parameters<Implementation[Key]>
      ) => ReturnType<Implementation[Key]>;
      return await method(...args);
    };
    // 同一 key 原样转发参数与结果；这里保留泛型索引与方法签名的对应关系。
    return invoke as Methods[Key];
  };
  // 显式枚举构成接口完整性检查；实现增加或移除操作时，由类型检查强制同步此边界。
  const methodNames = {
    listConnections: true,
    getCapabilities: true,
    assertCapability: true,
    listAvailableLibraries: true,
    testLibraryEmbedding: true,
    getLibrary: true,
    createLibrary: true,
    updateLibrary: true,
    deleteLibrary: true,
    removeMount: true,
    getLibraryDeletion: true,
    recoverLibraryDeletion: true,
    listSources: true,
    getSource: true,
    importSource: true,
    getUploadContext: true,
    redetectService: true,
    uploadSource: true,
    archiveSource: true,
    getRaw: true,
    getSourceChunks: true,
    previewSourcePurge: true,
    confirmSourcePurge: true,
    listSourcePurges: true,
    getSourcePurge: true,
    recoverSourcePurge: true,
    listJobs: true,
    getJob: true,
    resumeJob: true,
    retryJob: true,
    sendMaintenanceMessage: true,
    listPages: true,
    listPageDirectory: true,
    getPageBySlug: true,
    getWikiIndex: true,
    listThemes: true,
    createTheme: true,
    writeTheme: true,
    createPage: true,
    writePage: true,
    restorePage: true,
    getLibraryExecutionDefaults: true,
    updateLibraryExecutionDefaults: true,
    getLibraryEmbeddingProfile: true,
    updateLibraryEmbeddingProfile: true,
    listLibraryActivity: true,
    previewChunking: true,
    getPage: true,
    listRevisions: true,
    getRevision: true,
    organize: true,
    organizeRemote: true,
    getGraph: true,
    getRemoteGraph: true,
    getOperation: true,
    listOperations: true,
    getReceipt: true,
  } satisfies Record<MethodName, true>;
  const methods = Object.fromEntries(
    (Object.keys(methodNames) as MethodName[]).map((key) => [key, call(key)]),
  ) as Methods;
  return { ...knowledgeAdapterMetadata(ref), ...methods } as Implementation;
}
