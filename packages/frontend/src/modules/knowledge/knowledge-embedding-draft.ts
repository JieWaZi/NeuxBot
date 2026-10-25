/** 密钥仅是尚未提交的表单输入；GET 始终只返回是否已配置。 */
export type KnowledgeEmbeddingDraft = {
  baseUrl: string;
  model: string;
  apiKey: string;
  hasApiKey: boolean;
};
export function emptyEmbeddingDraft(): KnowledgeEmbeddingDraft {
  return { baseUrl: "", model: "", apiKey: "", hasApiKey: false };
}
