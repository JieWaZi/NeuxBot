import { usePluginResources } from "./use-plugin-resources";

/** 引用读取已启用的固定安装版本，真实配置与身份由执行接纳核验。 */
export function useInstalledPluginReferences(
  wakerId: string | undefined,
  active = true,
) {
  const query = usePluginResources(wakerId, active && Boolean(wakerId));
  const plugins = (query.data ?? []).flatMap((detail) => {
    const relation = detail.installations?.find(
      (item) => item.wakerId === wakerId,
    );
    const version = detail.versions?.find(
      (item) => item.id === relation?.versionId,
    );
    if (!relation?.enabled || !detail.plugin?.id || !version) return [];
    return [
      {
        id: detail.plugin.id,
        name: detail.plugin.name ?? "",
        description: detail.plugin.description ?? "",
        version: version.version,
        versionId: version.id,
      },
    ];
  });
  return { ...query, plugins };
}
