/** 返回资源库详情路径，伙伴标识仅用于确定连接器的安装上下文。 */
export function resourceLibraryDetailPath(
  category: "skills" | "connectors",
  resourceId: string,
  wakerId?: string,
): string {
  const search = wakerId ? `?${new URLSearchParams({ wakerId })}` : "";
  return `/resources/${category}/${encodeURIComponent(resourceId)}${search}`;
}

/** 返回仅落在当前资源列表范围，保留列表自己拥有的搜索与筛选参数。 */
export function resourceListBack(candidate: unknown, fallback: string): string {
  if (
    typeof candidate !== "string" ||
    !candidate.startsWith("/") ||
    candidate.startsWith("//")
  )
    return fallback;
  try {
    const destination = new URL(candidate, "https://resource.invalid");
    const expected = new URL(fallback, "https://resource.invalid");
    return destination.origin === expected.origin &&
      destination.pathname === expected.pathname
      ? `${destination.pathname}${destination.search}${destination.hash}`
      : fallback;
  } catch {
    return fallback;
  }
}
