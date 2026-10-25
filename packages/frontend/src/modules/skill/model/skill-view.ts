import type { GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse } from "@/shared/api/generated/types.gen";

import type { SkillSummary } from "./types";

export function toSkillSummary(
  item: GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse,
): SkillSummary | null {
  const skill = item.skill;
  if (!skill?.id) return null;
  const scope =
    skill.builtIn || skill.source === "builtin"
      ? "built_in"
      : skill.scope === "public"
        ? "public"
        : "private";
  const source =
    skill.source === "git" ? "git" : skill.source === "zip" ? "zip" : "ally";
  return {
    id: skill.id,
    name: skill.name ?? "",
    description: skill.description ?? "",
    version: item.currentVersion?.version ?? "",
    scope,
    source,
    enabled: item.enabled ?? false,
    affectedWakerCount: item.affectedWakerCount ?? 1,
    installedPartners: (item.installedPartners ?? []).flatMap((partner) =>
      partner.id
        ? [{ id: partner.id, name: partner.name ?? "", avatar: partner.avatar }]
        : [],
    ),
    updatedAt: new Date(skill.updatedAt ?? 0).toISOString(),
  };
}
