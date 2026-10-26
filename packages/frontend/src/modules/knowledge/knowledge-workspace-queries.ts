import { useQuery, useInfiniteQuery } from "@tanstack/react-query";
import {
  knowledgeAdapter,
  type KnowledgeSourceFilters,
} from "@/shared/api/knowledge";
import type { ResourceRef } from "./knowledge-transfer";
import {
  knowledgeKeys,
  jobsOptions,
  librariesOptions,
  libraryOptions,
  sourceDirectoryOptions,
} from "./knowledge-queries";
import type { LibraryLocation } from "./knowledge-model";

/** 只读取列表、文档与真实整理任务；页面可见时才对账。 */
export function useKnowledgeWorkspaceQueries({
  ref,
  detail,
  active,
  listOffset,
  librarySearch = "",
  sourceFilters,
  location,
}: {
  ref: ResourceRef;
  detail: boolean;
  active: boolean;
  listOffset: number;
  librarySearch?: string;
  sourceFilters: KnowledgeSourceFilters;
  location: LibraryLocation;
}) {
  const libraries = useQuery({
    ...librariesOptions(listOffset, location, librarySearch),
    enabled: active && !detail,
  });
  const libraryQuery = useQuery({
    ...libraryOptions(ref),
    enabled: active && detail,
  });
  const sources = useInfiniteQuery({
    ...sourceDirectoryOptions(ref, sourceFilters),
    // 三个目录分段共用原件总数，不能随原件列表隐藏而停用事件对账。
    enabled: active && detail && Boolean(ref.knowledgeBaseId),
  });
  const adapter = knowledgeAdapter(ref);
  const capabilities = useQuery({
    queryKey: knowledgeKeys.capabilities(ref),
    queryFn: ({ signal }) => adapter.getCapabilities(signal),
    enabled: active && detail && adapter.supportsServiceDetection,
  });
  const jobs = useQuery({
    ...jobsOptions(ref),
    enabled:
      active &&
      detail &&
      adapter.supportsJobs &&
      (!adapter.supportsServiceDetection ||
        capabilities.data?.uploadMode === "separate"),
  });
  const activeJob = jobs.data?.activeJob;
  return { libraries, libraryQuery, sources, jobs, activeJob, capabilities };
}
