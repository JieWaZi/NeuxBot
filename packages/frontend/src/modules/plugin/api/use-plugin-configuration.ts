import { useQuery } from "@tanstack/react-query";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { getPluginConfigurationOptions } from "./plugin-queries";

/** 设置与安装步骤按明确版本惰性读取，静态详情不持有配置值。 */
export function usePluginConfiguration(
  pluginId: string,
  versionId: string,
  wakerId = "",
  active = true,
) {
  const routeActive = useRouteSceneActive();
  const visible = active && routeActive;
  const options = getPluginConfigurationOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { versionId, wakerId },
    }),
  );
  const enabled = Boolean(pluginId && versionId);
  const query = useQuery({
    ...options,
    enabled: visible && enabled,
    refetchOnMount: "always",
    staleTime: 0,
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active: visible,
    enabled,
  });
  return query;
}
