import type { GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse } from "@/shared/api/generated/types.gen";

import type { SkillSummary } from "./types";

export function toSkillSummary(
  item: GithubComJieWaZiNeuxBotServerApisSkillsWakerSkillResponse,
): SkillSummary | null {
  const skill = item.skill;
  if (!skill?.id || (skill.source !== "git" && skill.source !== "zip"))
    return null;
  const source = skill.source;
  return {
    id: skill.id,
    name: skill.name ?? "",
    description: skill.description ?? "",
    categories: skill.categories,
    version: item.currentVersion?.version ?? "",
    source,
    sourceUrl: skill.gitUrl,
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
