export type SkillSource = "git" | "zip";

export type SkillSummary = {
  id: string;
  name: string;
  description: string;
  categories?: string[];
  version: string;
  source: SkillSource;
  sourceUrl?: string;
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
