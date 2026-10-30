import { useQuery } from "@tanstack/react-query";
import {
  listWakerPluginReferencesOptions,
  listWakerPluginReferencesQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { useQueryEvents } from "@/shared/api/use-query-events";

/** 输入候选只读取所选伙伴已启用的固定版本身份；实际执行仍由接纳核验。 */
export function useInstalledPluginReferences(
  wakerId: string | undefined,
  active = true,
) {
  const options = { path: { wakerId: wakerId ?? "pending" } };
  const query = useQuery({
    ...listWakerPluginReferencesOptions(options),
    enabled: active && Boolean(wakerId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [listWakerPluginReferencesQueryKey(options)],
    active,
    enabled: Boolean(wakerId),
    eventTypes: ["plugins.references.changed"],
    eventFilter: (frame) => {
      if (frame.resource.kind === "waker") return frame.resource.id === wakerId;
      if (frame.resource.kind === "plugin") {
        // 首份快照尚未确认时不能按空集合丢弃展示变更，旧 GET 必须补读。
        return (
          query.data === undefined ||
          query.data.some((plugin) => plugin.id === frame.resource.id)
        );
      }
      return frame.resource.kind === "global";
    },
  });
  const plugins = (query.data ?? []).flatMap((plugin) =>
    plugin.id && plugin.versionId
      ? [
          {
            id: plugin.id,
            name: plugin.name ?? "",
            description: plugin.description ?? "",
            version: plugin.version ?? "",
            versionId: plugin.versionId,
          },
        ]
      : [],
  );
  return { ...query, plugins };
}
