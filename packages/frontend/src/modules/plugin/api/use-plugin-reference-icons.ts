import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { listPluginReferenceIconsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListPluginReferenceIconsResponse } from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";

type PluginReferenceIcon = ListPluginReferenceIconsResponse[number];

/** 历史引用按实际缺失的资源标识批量补齐，只读取全局展示事实。 */
export function usePluginReferenceIcons(
  pluginIds: readonly string[],
  active: boolean,
) {
  const batches = useMemo(() => {
    const ids = [...new Set(pluginIds)].sort();
    const batches: string[][] = [];
    for (let offset = 0; offset < ids.length; offset += 100)
      batches.push(ids.slice(offset, offset + 100));
    return batches;
  }, [pluginIds]);
  const queries = useMemo(
    () =>
      batches.map((pluginId) => ({
        ...listPluginReferenceIconsOptions(
          withLocalErrorSurface({ query: { pluginId } }),
        ),
        enabled: active,
        staleTime: 60_000,
      })),
    [active, batches],
  );
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: queries.map((query) => query.queryKey),
    active,
    enabled: batches.length > 0,
    eventTypes: ["plugins.references.changed"],
    eventFilter: (frame) =>
      frame.resource.kind === "global" ||
      (frame.resource.kind === "plugin" &&
        pluginIds.includes(frame.resource.id ?? "")),
  });
  return useQueries({
    queries,
    combine: (results) => {
      const icons: Record<string, PluginReferenceIcon | null> =
        Object.create(null);
      for (const [index, result] of results.entries()) {
        if (!result.data) continue;
        for (const id of batches[index] ?? []) icons[id] = null;
        for (const icon of result.data) if (icon.id) icons[icon.id] = icon;
      }
      return icons;
    },
  });
}
