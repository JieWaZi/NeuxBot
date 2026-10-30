import { useQuery } from "@tanstack/react-query";
import { listPlugins, listWakerPlugins } from "@/shared/api/generated/sdk.gen";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { PluginDetail } from "../model/plugin-view";

/** 管理目录与组件详情读取完整插件快照。 */
export function usePluginResources(wakerId: string | undefined, active = true) {
  const queryKey = [
    {
      _id: wakerId ? "listWakerPluginResources" : "listPluginResources",
      path: { wakerId },
    },
  ] as const;
  const query = useQuery({
    queryKey,
    enabled: active,
    queryFn: async ({ signal }) => {
      const resources: PluginDetail[] = [];
      for (let page = 1; ; page++) {
        const options = withLocalErrorSurface({
          query: { pageNum: page, pageSize: 100 },
          signal,
          throwOnError: true as const,
        });
        const response = wakerId
          ? await listWakerPlugins({
              ...options,
              path: { wakerId },
              throwOnError: true,
            })
          : await listPlugins({ ...options, throwOnError: true });
        const batch = response.data.resources ?? [];
        resources.push(...batch);
        if (
          resources.length >= (response.data.totalSize ?? 0) ||
          batch.length === 0
        )
          break;
      }
      return resources;
    },
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [queryKey],
    active,
  });
  return query;
}
