import { describe, expect, it } from "vitest";

import {
  buildSkillResourceTree,
  countSkillResourceFiles,
  findSkillResourceByPath,
  isVersionGreater,
  suggestNextPatchVersion,
} from "./skill-detail";

describe("skill detail model", () => {
  it("按路径构建资源树并支持查找与文件计数", () => {
    const resources = buildSkillResourceTree([
      { path: "SKILL.md", directory: false, size: 20, digest: "skill" },
      { path: "references", directory: true },
      {
        path: "references/checklist.md",
        directory: false,
        size: 12,
        digest: "checklist",
      },
      { path: "scripts/review.sh", directory: false, size: 8 },
      { path: "  " },
    ]);

    expect(resources).toEqual([
      {
        name: "SKILL.md",
        path: "SKILL.md",
        type: "file",
        size: 20,
        digest: "skill",
      },
      {
        name: "references",
        path: "references",
        type: "directory",
        children: [
          {
            name: "checklist.md",
            path: "references/checklist.md",
            type: "file",
            size: 12,
            digest: "checklist",
          },
        ],
      },
      {
        name: "scripts",
        path: "scripts",
        type: "directory",
        children: [
          {
            name: "review.sh",
            path: "scripts/review.sh",
            type: "file",
            size: 8,
            digest: undefined,
          },
        ],
      },
    ]);
    expect(
      findSkillResourceByPath(resources, "references/checklist.md"),
    ).toEqual(expect.objectContaining({ name: "checklist.md", size: 12 }));
    expect(findSkillResourceByPath(resources, "missing.md")).toBeUndefined();
    expect(countSkillResourceFiles(resources)).toBe(3);
  });

  it("按严格三段 SemVer 生成补丁版本并判断递增", () => {
    expect(suggestNextPatchVersion("1.4.9")).toBe("1.4.10");
    expect(suggestNextPatchVersion("invalid")).toBe("1.0.0");
    expect(isVersionGreater("2.0.0", "1.99.99")).toBe(true);
    expect(isVersionGreater("1.4.1", "1.4.0")).toBe(true);
    expect(isVersionGreater("1.4.0", "1.4.0")).toBe(false);
    expect(isVersionGreater("1.4.0-beta", "1.4.0")).toBe(false);
    expect(
      isVersionGreater(
        "9223372036854775808.0.0",
        "9223372036854775807.999.999",
      ),
    ).toBe(true);
    expect(suggestNextPatchVersion("1.2.18446744073709551616")).toBe(
      "1.2.18446744073709551617",
    );
  });
});
