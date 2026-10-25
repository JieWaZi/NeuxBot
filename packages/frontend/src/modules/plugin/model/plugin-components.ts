import {
  pluginDisplayName,
  pluginInstalledVersion,
  pluginSummary,
  type PluginDetail,
} from "./plugin-view";

/** 伙伴只展示其固定版本，资源库展示本机当前版本；使用数按包含同一组件的安装计算。 */
export function pluginComponents(
  resources: PluginDetail[],
  kind: "skills" | "connectors",
  search: string,
  wakerId = "",
) {
  const needle = search.trim().toLocaleLowerCase();
  return resources.flatMap((detail) => {
    const relation = detail.installations?.find(
      (item) => item.wakerId === wakerId,
    );
    const version = wakerId
      ? detail.versions?.find((item) => item.id === relation?.versionId)
      : pluginInstalledVersion(detail);
    const pkg = version?.package;
    const pluginId = detail.plugin?.id;
    if (!pluginId || !version?.id || !pkg || (wakerId && !relation)) return [];
    const versionId = version.id;
    const sourceName = pluginDisplayName(pkg, detail.plugin?.name);
    const entries =
      kind === "skills"
        ? (pkg.skills ?? []).map((skill) => ({
            key: skill.key,
            name: skill.name,
            description: skill.description,
          }))
        : (pkg.servers ?? []).map((server) => ({
            key: server.key,
            name: server.key,
            description:
              pluginSummary(
                pkg,
                detail.plugin?.description,
                detail.plugin?.sourceKey,
              ) ||
              server.url ||
              server.command,
          }));
    return entries.flatMap((entry) => {
      if (
        !entry.key ||
        !entry.name ||
        (needle &&
          ![entry.name, entry.description, sourceName]
            .join(" ")
            .toLocaleLowerCase()
            .includes(needle))
      )
        return [];
      const installations = (detail.installations ?? []).filter((item) => {
        const installed = detail.versions?.find(
          (candidate) => candidate.id === item.versionId,
        )?.package;
        return kind === "skills"
          ? installed?.skills?.some((skill) => skill.key === entry.key)
          : installed?.servers?.some((server) => server.key === entry.key);
      });
      return [
        {
          ...entry,
          key: entry.key,
          name: entry.name,
          id: `${pluginId}:${kind}:${entry.key}`,
          pluginId,
          versionId,
          sourceName,
          installations,
          detail,
        },
      ];
    });
  });
}
