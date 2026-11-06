import { resourceCategory } from "@/modules/plugin";

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
  for (const key of ["page", "search", "view", "componentPage", "source"])
    next.delete(key);
  return next;
}
