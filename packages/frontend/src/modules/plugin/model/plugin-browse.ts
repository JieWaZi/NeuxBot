import type { BrowsePluginsResponse } from "@/shared/api/generated/types.gen";

export type PluginBrowseEntry = NonNullable<
  BrowsePluginsResponse["entries"]
>[number];

/** 浏览分组沿用参考目录顺序；包的自由分类通过显式宿主映射归组。 */
export const PLUGIN_BROWSE_CATEGORIES = [
  "coding",
  "design",
  "data-analytics",
  "documents",
  "productivity",
  "research",
  "automation",
  "other",
] as const;

export const PLUGIN_CATEGORY_LABELS = {
  coding: "categoryCoding",
  design: "categoryDesign",
  "data-analytics": "categoryDataAnalytics",
  documents: "categoryDocuments",
  productivity: "categoryProductivity",
  research: "categoryResearch",
  automation: "categoryAutomation",
  other: "categoryOther",
} as const;

// 这是宿主的显示分组映射；标准包 category 是自由字符串，不是官方枚举。
const HOST_CATEGORY_ALIASES: Readonly<
  Record<string, (typeof PLUGIN_BROWSE_CATEGORIES)[number]>
> = {
  coding: "coding",
  "developer tools": "coding",
  design: "design",
  "data-analytics": "data-analytics",
  "data & analytics": "data-analytics",
  documents: "documents",
  productivity: "productivity",
  research: "research",
  automation: "automation",
  other: "other",
};
export function pluginBrowseCategory(category: string | undefined) {
  const key = category?.trim().toLowerCase() ?? "";
  return Object.hasOwn(HOST_CATEGORY_ALIASES, key)
    ? (HOST_CATEGORY_ALIASES[key] ?? "other")
    : "other";
}

export function pluginBrowseGroups(entries: readonly PluginBrowseEntry[]) {
  return PLUGIN_BROWSE_CATEGORIES.map((category) => ({
    category,
    entries: entries.filter(
      (entry) => pluginBrowseCategory(entry.category) === category,
    ),
  })).filter((group) => group.entries.length > 0);
}

/** 本地插件始终进入真实详情；目录包只有实际安装后才从固定版本进入。 */
export function pluginBrowseHref(entry: PluginBrowseEntry): string {
  return entry.pluginId &&
    ((entry.installedCount ?? 0) > 0 || !entry.catalogKey)
    ? `/resources/plugins/${encodeURIComponent(entry.pluginId)}`
    : `/resources/plugins/catalog/${encodeURIComponent(entry.catalogKey ?? "")}`;
}
