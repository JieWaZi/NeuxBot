import { queryOptions } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { GetLibraryEmbeddingProfileResponse } from "@/shared/api/generated/types.gen";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";

import type { KnowledgeEmbeddingDraft } from "./knowledge-embedding-draft";
export {
  emptyEmbeddingDraft,
  type KnowledgeEmbeddingDraft,
} from "./knowledge-embedding-draft";

export function readEmbeddingProfile(
  profile: GetLibraryEmbeddingProfileResponse,
): KnowledgeEmbeddingDraft {
  return {
    baseUrl: profile.baseUrl ?? "",
    model: profile.model ?? "",
    apiKey: "",
    hasApiKey: Boolean(profile.hasApiKey),
  };
}
export function libraryEmbeddingProfileOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "embedding-profile"],
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter(ref).getLibraryEmbeddingProfile({
          path: { libraryId: ref.knowledgeBaseId },
          signal,
          throwOnError: true,
        })
      ).data,
    staleTime: 0,
  });
}
