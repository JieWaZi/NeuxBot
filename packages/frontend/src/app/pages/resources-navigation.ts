import { resourceCategory } from "@/modules/plugin";
import { resourceListBack } from "@/shared/lib/resource-navigation";

export function resourceCategoryFromParams(
  params: URLSearchParams,
  wakerId = "",
) {
  return !wakerId && params.get("category") === "workflows"
    ? "workflows"
    : resourceCategory(params.get("category"));
}

/** 分类变更沿用资源页的条件清理语义，伙伴目标及其他有效参数保持原样。 */
export function changeResourceCategory(
  params: URLSearchParams,
  category: string,
) {
  const next = new URLSearchParams(params);
  next.set("category", category);
  for (const key of ["page", "search", "componentPage", "source"])
    next.delete(key);
  if (category !== "skills") next.delete("skillCategory");
  return next;
}

/** 快捷项沿用原列表入口；直接详情恢复列表支持的搜索、技能分类与目标伙伴。 */
export function resourceLibraryHref(location: {
  pathname: string;
  search: string;
  state: unknown;
}): string {
  if (location.pathname === "/resources")
    return location.pathname + location.search;
  const params = new URLSearchParams(location.search);
  const fallback = new URLSearchParams({
    category: location.pathname.startsWith("/resources/skills/")
      ? "skills"
      : location.pathname.startsWith("/resources/connectors/")
        ? "connectors"
        : "plugins",
  });
  for (const key of ["search", "partner"]) {
    const value = params.get(key);
    if (value) fallback.set(key, value);
  }
  const skillCategory = params.get("skillCategory");
  if (skillCategory !== null) fallback.set("skillCategory", skillCategory);
  const state = location.state;
  return resourceListBack(
    state && typeof state === "object" && "resourceBack" in state
      ? state.resourceBack
      : undefined,
    resourceListBack(
      state && typeof state === "object" && "pluginResourceBack" in state
        ? state.pluginResourceBack
        : undefined,
      `/resources?${fallback.toString()}`,
    ),
  );
}
