import { expect, it } from "vitest";
import {
  pluginSummary,
  pluginDisplayName,
  pluginExplanation,
  pluginHasAuthorization,
  pluginNeedsAuthorization,
  type PluginCatalogEntry,
} from "./plugin-view";

const pkg: PluginCatalogEntry["package"] = {
  manifest: {
    name: "github",
    repository: "https://github.com/github/github-mcp-server",
    description: "旧版摘要",
    version: "1.0.1",
  },
  skills: [],
  servers: [{ key: "github", url: "https://api.githubcopilot.com/mcp/" }],
  readme: "旧版构建与隔离细节",
};

it("keeps the selected source README and summary when a generated guide has matching capabilities", () => {
  expect(pluginSummary(pkg, "本地摘要", "catalog:github")).toBe("旧版摘要");
  expect(pluginExplanation(pkg, "catalog:github")).toBe("旧版构建与隔离细节");
  expect(pkg.readme).toBe("旧版构建与隔离细节");
  expect(pkg.manifest?.version).toBe("1.0.1");
});

it("recognizes authorization declarations without a provider allowlist", () => {
  expect(
    pluginHasAuthorization({
      integration: {
        authentication: {
          strategy: "plugin-command",
          protocol: "neuxally-auth/1",
          command: "./.neuxally/auth/authorize",
        },
      },
    }),
  ).toBe(true);
  expect(pluginHasAuthorization({ manifest: { name: "skills-only" } })).toBe(
    false,
  );
});

it("keeps third-party and mismatching snapshots on their own documentation", () => {
  expect(pluginExplanation(pkg)).toBe(pkg.readme);
  expect(pluginExplanation({ ...pkg, servers: [] }, "catalog:github")).toBe(
    pkg.readme,
  );
});

it("uses the same summary fallback for every entry and never substitutes the author", () => {
  const entry = {
    manifest: { description: "插件能力", author: { name: "作者" } },
    presentation: { shortDescription: " ", developerName: "开发者" },
  };
  expect(pluginSummary(entry)).toBe("插件能力");
  expect(
    pluginSummary({ presentation: { developerName: "开发者" } }, "保存的简介"),
  ).toBe("保存的简介");
});

it("localizes authored names and descriptions while preserving a user rename", () => {
  const bilingual = {
    manifest: { name: "feishu-office", description: "Office tools" },
    presentation: {
      displayName: "Feishu Office",
      displayNames: { en: "Feishu Office", "zh-CN": "飞书办公" },
      localizedDescriptions: {
        en: "Work with documents",
        "zh-CN": "处理办公资料",
      },
    },
  };
  expect(pluginDisplayName(bilingual, "Feishu Office", "zh-CN")).toBe(
    "飞书办公",
  );
  expect(pluginDisplayName(bilingual, "我的办公室", "en")).toBe("我的办公室");
  expect(pluginDisplayName(bilingual, "飞书办公", "en")).toBe("Feishu Office");
  expect(
    pluginDisplayName(
      {
        ...bilingual,
        presentation: {
          ...bilingual.presentation,
          displayName: "feishu-office",
        },
      },
      "Feishu Office",
      "zh-CN",
    ),
  ).toBe("飞书办公");
  expect(pluginSummary(bilingual, undefined, undefined, "zh-CN")).toBe(
    "处理办公资料",
  );
  expect(pluginSummary(bilingual, undefined, undefined, "en")).toBe(
    "Work with documents",
  );
});

it("does not request reauthorization for an installed identity before its first check", () => {
  expect(
    pluginNeedsAuthorization(pkg, {
      installations: [{ wakerId: "w1" }],
      account: { available: false },
    }),
  ).toBe(false);
  expect(
    pluginNeedsAuthorization(pkg, {
      installations: [{ wakerId: "w1" }],
      account: { available: false, checkedAt: 123 },
    }),
  ).toBe(true);
  expect(
    pluginNeedsAuthorization(pkg, {
      installations: [{ wakerId: "w1" }],
      account: { available: true, checkedAt: 123 },
    }),
  ).toBe(false);
});
