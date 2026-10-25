export type SkillScope = "built_in" | "private" | "public";

export type SkillSource = "ally" | "git" | "zip";

export type SkillSummary = {
  id: string;
  name: string;
  description: string;
  version: string;
  scope: SkillScope;
  source: SkillSource;
  enabled: boolean;
  affectedWakerCount: number;
  installedPartners?: { id: string; name: string; avatar?: string }[];
  updatedAt: string;
};

export type SkillResource = {
  name: string;
  type: "directory" | "file";
  path: string;
  size?: number;
  digest?: string;
  executable?: boolean;
  children?: SkillResource[];
};
