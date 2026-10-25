import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginSkillDialog } from "./plugin-skill-dialog";
const read = vi.hoisted(() => vi.fn());
vi.mock("../api/plugin-queries", () => ({
  getPluginSkillOptions: (options: {
    query: { versionId: string; path?: string };
  }) => ({
    queryKey: ["skill", options.query.versionId, options.query.path],
    queryFn: () => read(options.query),
  }),
  getCatalogPluginSkillOptions: () => ({
    queryKey: ["catalog-disabled"],
    queryFn: () => read({ catalog: true }),
  }),
}));
afterEach(cleanup);
it("shows metadata and instructions for the selected plugin version without management tabs", async () => {
  const skill = { key: "guide", name: "guide", description: "Use the guide" };
  read.mockImplementation(
    async ({ versionId, path }: { versionId: string; path?: string }) => ({
      skill,
      content:
        versionId === "old"
          ? "# Previous guide"
          : "---\nname: guide\n---\n# Current guide",
      resources: [
        { path: "SKILL.md" },
        { path: "references", directory: true },
        { path: "references/checklist.md" },
      ],
      file: path ? { path, content: "# Checklist" } : undefined,
      version: {
        id: versionId,
        version: versionId === "old" ? "1.0.0" : "2.0.0",
        skill,
      },
      versions: [
        { id: "current", version: "2.0.0", skill },
        { id: "old", version: "1.0.0", skill },
      ],
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <PluginSkillDialog
        context={{ pluginId: "p", versionId: "current" }}
        skillKey="guide"
        name="guide"
        sourceName="办公插件"
        onClose={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole(
      "heading",
      { name: "Current guide" },
      { timeout: 5000 },
    ),
  ).toBeVisible();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "元数据" })).toBeVisible();
  expect(screen.getByText("办公插件")).toBeVisible();
  expect(screen.getByText("Use the guide")).toBeVisible();
  expect(screen.queryByText("版本历史")).not.toBeInTheDocument();
  expect(read).toHaveBeenCalledWith({
    versionId: "current",
    key: "guide",
    wakerId: undefined,
  });
});

it("shows catalog skill metadata with the plugin display name as its source", async () => {
  read.mockResolvedValue({
    skill: { name: "catalog-guide", description: "目录技能描述" },
    content: "# 目录技能正文",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <PluginSkillDialog
        context={{ catalogKey: "office" }}
        skillKey="guide"
        name="guide"
        sourceName="飞书办公"
        onClose={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("heading", { name: "目录技能正文" }),
  ).toBeVisible();
  expect(screen.getByText("catalog-guide")).toBeVisible();
  expect(screen.getByText("飞书办公")).toBeVisible();
  expect(screen.getByText("目录技能描述")).toBeVisible();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
});
