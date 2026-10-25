import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { pluginComponents } from "../model/plugin-components";
import { usePluginResources } from "./use-plugin-resources";

export function usePluginComponents(
  kind: "skills" | "connectors",
  wakerId: string,
  search: string,
  enabled = true,
) {
  const active = useRouteSceneActive();
  const query = usePluginResources(wakerId || undefined, active && enabled);
  return {
    ...query,
    items: pluginComponents(query.data ?? [], kind, search, wakerId),
  };
}
