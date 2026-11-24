export type PartnerReviewTab = "memory" | "skill";

/** 伙伴资源入口定位设置二级页内的功能与伙伴。 */
export function partnerReviewSettingsHref(
  wakerId?: string,
  tab: PartnerReviewTab = "memory",
) {
  const params = new URLSearchParams({ detail: "partner-review", tab });
  if (wakerId) params.set("wakerId", wakerId);
  return `/settings/agents?${params}`;
}

export function isPartnerReviewDetail(params: URLSearchParams) {
  return (
    params.get("detail") === "partner-review" ||
    (!params.has("agent") && params.has("wakerId"))
  );
}
