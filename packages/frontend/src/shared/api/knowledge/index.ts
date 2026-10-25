import type { knowledgeAdapter } from "./knowledge-adapter";

export { knowledgeAdapter } from "./knowledge-adapter";
export {
  knowledgeClient,
  KNOWLEDGE_SOURCE_PAGE_SIZE,
} from "./knowledge-client";
export type { KnowledgeSourceFilters } from "./knowledge-client";
export type {
  KnowledgeCapabilities,
  KnowledgeUploadContext,
  ResourceRef,
} from "./knowledge-adapter-types";

/** 本地与远端共同的领域操作边界；调用返回统一事实与错误语义。 */
export type KnowledgeAdapter = ReturnType<typeof knowledgeAdapter>;
