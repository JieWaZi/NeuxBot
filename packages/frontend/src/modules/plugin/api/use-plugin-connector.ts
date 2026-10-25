import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  getPluginConnectorOptions,
  getCatalogPluginConnectorOptions,
  testPluginConnectorMutation,
} from "./plugin-queries";
import type { PluginSkillContext } from "../components/plugin-skill-dialog";

/** 查看连接器只读取当前入口固定的来源，不触发连接或授权。 */
export function usePluginConnector(
  context: PluginSkillContext,
  connectorKey: string,
  active = true,
) {
  const client = useQueryClient();
  const catalog = Boolean(context.catalogKey);
  const catalogOptions = getCatalogPluginConnectorOptions(
    withLocalErrorSurface({
      path: { key: context.catalogKey ?? "" },
      query: {
        connectorKey,
        sourceCommit: context.sourceCommit,
        archiveSHA256: context.archiveSHA256,
      },
    }),
  );
  const installedOptions = getPluginConnectorOptions(
    withLocalErrorSurface({
      path: { pluginId: context.pluginId ?? "" },
      query: {
        versionId: context.versionId ?? "",
        key: connectorKey,
        wakerId: context.wakerId || undefined,
      },
    }),
  );
  const catalogQuery = useQuery({
    ...catalogOptions,
    enabled: active && catalog && Boolean(connectorKey),
  });
  const installedQuery = useQuery({
    ...installedOptions,
    enabled:
      active &&
      !catalog &&
      Boolean(connectorKey && context.pluginId && context.versionId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [catalog ? catalogOptions.queryKey : installedOptions.queryKey],
    active,
  });
  const probe = useMutation(
    withLocalErrorMutation(testPluginConnectorMutation()),
  );
  const query = catalog ? catalogQuery : installedQuery;
  const canTest = Boolean(context.pluginId && context.versionId);
  const test = () => {
    if (!canTest) return;
    // 回调固定发起时的查询身份，切换组件或版本后不会把迟到结果写入新详情。
    const viewKey = catalog
      ? catalogOptions.queryKey
      : installedOptions.queryKey;
    const savedKey = installedOptions.queryKey;
    probe.mutate(
      {
        path: { pluginId: context.pluginId ?? "" },
        query: {
          versionId: context.versionId ?? "",
          key: connectorKey,
          wakerId: context.wakerId || undefined,
          sourceCommit: context.catalogKey ? context.sourceCommit : undefined,
          archiveSHA256: context.catalogKey ? context.archiveSHA256 : undefined,
        },
      },
      {
        onSuccess: async () => {
          await client.invalidateQueries({ queryKey: savedKey });
          if (catalog) await client.invalidateQueries({ queryKey: viewKey });
        },
      },
    );
  };
  const currentProbe =
    probe.variables?.path?.pluginId === context.pluginId &&
    probe.variables?.query?.versionId === context.versionId &&
    probe.variables?.query?.key === connectorKey &&
    probe.variables?.query?.wakerId === (context.wakerId || undefined);
  return {
    ...query,
    canTest,
    test,
    testing: currentProbe && probe.isPending,
    testError: currentProbe ? probe.error : null,
  };
}
