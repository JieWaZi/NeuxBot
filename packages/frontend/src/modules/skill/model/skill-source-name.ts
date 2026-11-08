import type { TFunction } from "i18next";
import type { SkillSummary } from "./types";

/** Git 来源保留仓库身份，避免同一托管站点下的不同来源合并成域名。 */
export function skillSourceName(
  skill: Pick<SkillSummary, "source" | "sourceUrl">,
  t: TFunction<"skill">,
): string {
  if (skill.source === "git" && skill.sourceUrl) {
    const remote = skill.sourceUrl.trim();
    const scp = remote.match(/^[^@\s]+@([^:/]+):(.+)$/);
    if (scp) return `${scp[1]}/${scp[2]}`.replace(/\.git\/?$/, "");
    try {
      const url = new URL(remote);
      return `${url.host}${url.pathname}`
        .replace(/\.git\/?$/, "")
        .replace(/\/$/, "");
    } catch {
      return remote;
    }
  }
  return t(
    skill.source === "zip" ? "source.zipImport" : `source.${skill.source}`,
  );
}
