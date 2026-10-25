import { queryOptions } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { GetLibraryExecutionDefaultsResponse } from "@/shared/api/generated/types.gen";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { ResourceRef } from "./knowledge-transfer";
import { knowledgeKeys } from "./knowledge-queries";

export function libraryExecutionDefaultsOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "execution-defaults"],
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter(ref).getLibraryExecutionDefaults({
          path: { libraryId: ref.knowledgeBaseId },
          signal,
          throwOnError: true,
        })
      ).data,
    staleTime: 0,
  });
}

export function readExecutionDefaults(
  value: GetLibraryExecutionDefaultsResponse,
): ExecutionSelection {
  if (!value.version) return {};
  return {
    runtimeId: value.runtimeId ?? "",
    accountId: value.accountId ?? "",
    model: value.model ?? "",
    reasoning: value.reasoning ?? "",
  };
}

export { buildExecutionDefaults } from "./knowledge-execution-config";
