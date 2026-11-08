import { client } from "@/shared/api/generated/client.gen";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll } from "vitest";
const categoryServer = setupServer(
  http.get("http://127.0.0.1:8080/api/v1/resource-categories", () =>
    HttpResponse.json([{ id: "category", name: "开发" }]),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  categoryServer.listen({ onUnhandledRequest: "error" });
});
afterAll(() => categoryServer.close());
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { SkillDetailPage } from "./skill-detail-page";

const {
  createVersion,
  pullUpdate,
  restoreVersion,
  saveBlob,
  setEnabled,
  uninstallSkill,
  updateSkill,
  themeState,
  pageFixture,
} = vi.hoisted(() => ({
  createVersion: vi.fn<
    (options: Record<string, unknown>) => Promise<{ id: string }>
  >(async () => ({ id: "version-2" })),
  restoreVersion: vi.fn<
    (options: Record<string, unknown>) => Promise<{ id: string }>
  >(async () => ({ id: "version-restored" })),
  pullUpdate: vi.fn<
    (options: Record<string, unknown>) => Promise<{ id: string }>
  >(async () => ({ id: "pulled" })),
  saveBlob: vi.fn<(blob: Blob, fileName: string) => Promise<boolean>>(
    async () => true,
  ),
  setEnabled: vi.fn<
    (options: Record<string, unknown>) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
  uninstallSkill: vi.fn<
    (options: Record<string, unknown>) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
  updateSkill: vi.fn<
    (options: Record<string, unknown>) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
  themeState: { resolvedTheme: "light" as "dark" | "light" },
  pageFixture: {
    pending: false,
    partners: [{ id: "waker-1", name: "代码伙伴" }],
  },
}));

vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@/shared/platform", () => ({ saveBlob }));
vi.mock("@/shared/theme", () => ({ useTheme: () => themeState }));

vi.mock("@git-diff-view/react", () => ({
  DiffModeEnum: { Unified: "unified" },
  DiffView: ({ diffViewTheme }: { diffViewTheme: string }) => (
    <div data-testid="skill-diff" data-theme={diffViewTheme} />
  ),
}));

vi.mock("../api/skill-queries", () => ({
  getWakerSkillOptions: () => ({
    queryKey: ["skill", "code-review"],
    queryFn: async () =>
      pageFixture.pending
        ? new Promise<never>(() => undefined)
        : {
            skill: {
              id: "code-review",
              name: "代码审查",
              description: "检查真实改动",
              categories: ["开发"],
              source: "git",
              currentVersionId: "version-1",
            },
            currentVersion: {
              id: "version-1",
              version: "1.4.0",
              name: "code-review",
              description: "Review real changes",
              operation: "import",
              createdAt: 1,
            },
            enabled: true,
            installedPartners: pageFixture.partners,
            affectedWakerCount: pageFixture.partners.length,
          },
  }),
  getWakerSkillQueryKey: () => ["skill", "code-review"],
  listSkillVersionsOptions: () => ({
    queryKey: ["skill-versions", "code-review"],
    queryFn: async () => [
      {
        id: "version-1",
        version: "1.4.0",
        operation: "import",
        createdAt: 1,
      },
      {
        id: "version-0",
        version: "1.3.0",
        operation: "edit",
        createdAt: 0,
      },
    ],
  }),
  listSkillVersionsQueryKey: () => ["skill-versions", "code-review"],
  getSkillVersionOptions: ({ path }: { path: { versionId: string } }) => ({
    queryKey: ["skill-version", path.versionId],
    queryFn: async () =>
      path.versionId === "version-0"
        ? {
            version: { id: "version-0", version: "1.3.0" },
            content: "# 旧版代码审查",
            resources: [
              {
                path: "SKILL.md",
                directory: false,
                digest: "old-skill",
                size: 18,
              },
              { path: "references", directory: true },
              {
                path: "references/checklist.md",
                directory: false,
                digest: "old-checklist",
                size: 8,
              },
            ],
          }
        : {
            version: { id: "version-1", version: "1.4.0" },
            content: "# 代码审查\n\n检查真实改动。",
            resources: [
              {
                path: "SKILL.md",
                directory: false,
                digest: "current-skill",
                size: 20,
              },
              { path: "references", directory: true },
              {
                path: "references/checklist.md",
                directory: false,
                digest: "current-checklist",
                size: 12,
              },
              { path: "scripts", directory: true },
              {
                path: "scripts/review.sh",
                directory: false,
                digest: "current-script",
                executable: true,
                size: 20,
              },
            ],
          },
  }),
  getSkillVersionFileOptions: ({
    path,
    query,
  }: {
    path: { versionId: string };
    query: { path: string };
  }) => ({
    queryKey: ["skill-version-file", path.versionId, query.path],
    queryFn: async () => ({
      path: query.path,
      content:
        path.versionId === "version-0"
          ? "- 检查旧实现"
          : "- 检查当前实现\n- 运行测试",
      binary: false,
      size: path.versionId === "version-0" ? 8 : 12,
    }),
  }),
  createSkillVersionMutation: () => ({ mutationFn: createVersion }),
  setWakerSkillEnabledMutation: () => ({ mutationFn: setEnabled }),
  restoreSkillVersionMutation: () => ({ mutationFn: restoreVersion }),
  pullSkillUpdateMutation: () => ({ mutationFn: pullUpdate }),
  uninstallSkillMutation: () => ({ mutationFn: uninstallSkill }),
  updateWakerSkillMutation: () => ({ mutationFn: updateSkill }),
  listWakerSkillsQueryKey: () => ["waker-skills", "waker-1"],
  invalidateVisibleSkillQueries: async () => undefined,
}));

beforeEach(() => {
  pageFixture.pending = false;
  pageFixture.partners = [{ id: "waker-1", name: "代码伙伴" }];
  themeState.resolvedTheme = "light";
  delete document.documentElement.dataset.theme;
  createVersion.mockClear();
  updateSkill.mockClear();
  pullUpdate.mockClear();
  restoreVersion.mockClear();
  saveBlob.mockClear();
  setEnabled.mockReset();
  setEnabled.mockResolvedValue({ success: true });
  uninstallSkill.mockReset();
  uninstallSkill.mockResolvedValue({ success: true });
});

function renderPage(
  path = "/wakers/waker-1/skills/code-review",
  resourceBack?: string,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[{ pathname: path, state: { resourceBack } }]}
      >
        <Routes>
          <Route
            path="/wakers/:wakerId/skills/:skillId"
            element={<SkillDetailPage />}
          />
          <Route
            path="/resources/skills/:skillId"
            element={<SkillDetailPage />}
          />
          <Route
            path="/wakers/:wakerId/resources"
            element={<div>技能列表页</div>}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("SkillDetailPage", () => {
  it("follows the skill page order with partner controls and document-local navigation", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "代码审查" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "代码审查" }).closest("header"),
    ).not.toHaveClass("border-b");
    expect(screen.queryByText("code-review")).not.toBeInTheDocument();
    expect(screen.getByText("Review real changes")).toBeInTheDocument();
    await screen.findByText(
      "检查真实改动。",
      { exact: true },
      { timeout: 5000 },
    );
    expect(
      screen.getByRole("button", { name: "返回技能列表" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "SKILL.md" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(
      screen.getByRole("radio", { name: "资源文件 3" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "版本" })).toBeInTheDocument();

    expect(
      screen.getByRole("radiogroup", { name: "技能详情内容" }),
    ).toBeInTheDocument();
    const partnerSection = screen
      .getByRole("heading", { name: "伙伴" })
      .closest("section");
    const documentSection = screen.getByRole("radiogroup").closest("section");
    expect(partnerSection).toContainElement(
      screen.getByRole("switch", { name: "停用 代码审查" }),
    );
    expect(documentSection).toContainElement(
      screen.getByRole("radio", { name: "资源文件 3" }),
    );
    expect(
      screen.queryByRole("heading", { name: "管理" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "版本" }));
    expect(
      screen.getByRole("heading", { name: "版本历史" }),
    ).toBeInTheDocument();
  });

  it("mounts all content tabs before the skill or file count is available", async () => {
    pageFixture.pending = true;
    const user = userEvent.setup();
    renderPage();
    expect(screen.getByRole("radio", { name: "SKILL.md" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "资源文件 0" })).toBeEnabled();
    expect(screen.getByRole("radio", { name: "版本" })).toBeEnabled();
    await user.click(screen.getByRole("radio", { name: "资源文件 0" }));
    expect(screen.getByRole("radio", { name: "资源文件 0" })).toBeChecked();
  });

  it("从标题区返回当前伙伴的技能列表", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.hover(screen.getByRole("button", { name: "返回技能列表" }));
    await user.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText("技能列表页")).toBeInTheDocument();
  });

  it("标题返回拒绝来源不明的地址，与面包屑使用相同列表入口", async () => {
    const user = userEvent.setup();
    renderPage(
      "/wakers/waker-1/skills/code-review",
      "https://external.test/unsafe",
    );
    await user.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText("技能列表页")).toBeInTheDocument();
  });

  it("说明只读，SKILL.md 在资源文件中编辑并保存新版本", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await screen.findByText(
      "检查真实改动。",
      { exact: true },
      { timeout: 5000 },
    );

    expect(
      screen.queryByRole("button", { name: "编辑 SKILL.md" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "资源文件 3" }));
    await user.click(screen.getByRole("button", { name: "SKILL.md" }));
    expect(
      screen.queryByRole("button", { name: "删除资源" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "重命名或移动" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "编辑 SKILL.md" }));
    const instructions = screen.getByRole("textbox", { name: "编辑 SKILL.md" });
    await user.clear(instructions);
    await user.type(instructions, "# 新说明");
    await user.click(screen.getByRole("button", { name: "应用文件更改" }));
    const saveArea = screen.getByLabelText("资源更改保存区");
    expect(
      saveArea.closest('[data-slot="resource-page-footer-host"]'),
    ).not.toBeNull();
    expect(saveArea.closest('[data-slot="resource-page-viewport"]')).toBeNull();
    await user.click(screen.getByRole("radio", { name: "SKILL.md" }));
    expect(screen.queryByLabelText("资源更改保存区")).not.toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "资源文件 3" }));
    expect(screen.getByLabelText("资源更改保存区")).toBeVisible();
    expect(screen.getByText("# 新说明")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "保存资源更改" }));

    await waitFor(() => expect(createVersion).toHaveBeenCalledTimes(1));
    expect(createVersion.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "code-review" },
        body: expect.objectContaining({
          content: "# 新说明",
          version: "1.4.1",
        }),
      }),
    );
  });

  it("重置资源草稿后收起底部保存区，不创建版本", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));
    await user.click(screen.getByRole("button", { name: "SKILL.md" }));
    await user.click(screen.getByRole("button", { name: "编辑 SKILL.md" }));
    const editor = screen.getByRole("textbox", { name: "编辑 SKILL.md" });
    await user.type(editor, "新的说明");
    await user.click(screen.getByRole("button", { name: "应用文件更改" }));
    const footer = screen.getByLabelText("资源更改保存区");
    await user.clear(within(footer).getByRole("textbox", { name: "新版本号" }));
    await user.type(
      within(footer).getByRole("textbox", { name: "新版本号" }),
      "1.0.0",
    );
    expect(
      within(footer).getByRole("button", { name: "保存资源更改" }),
    ).toBeDisabled();
    await user.click(within(footer).getByRole("button", { name: "重置" }));
    expect(screen.queryByLabelText("资源更改保存区")).not.toBeInTheDocument();
    expect(createVersion).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "SKILL.md" }));
    await user.click(screen.getByRole("button", { name: "编辑 SKILL.md" }));
    expect(
      screen.getByRole("textbox", { name: "编辑 SKILL.md" }),
    ).not.toHaveValue(expect.stringContaining("新的说明"));
  });

  it.each([false, true])(
    "内容未修改或编辑后还原时不产生待保存更改（还原：%s）",
    async (restore) => {
      const user = userEvent.setup();
      renderPage();
      await user.click(
        await screen.findByRole("radio", { name: "资源文件 3" }),
      );
      await user.click(screen.getByRole("button", { name: "checklist.md" }));
      await screen.findByText("- 检查当前实现", { exact: false });
      await user.click(
        screen.getByRole("button", { name: "编辑 references/checklist.md" }),
      );
      const editor = screen.getByRole("textbox", {
        name: "编辑 references/checklist.md",
      });
      const original = (editor as HTMLTextAreaElement).value;
      if (restore) {
        await user.type(editor, "更改");
        await user.clear(editor);
        await user.type(editor, original);
      }
      await user.click(screen.getByRole("button", { name: "应用文件更改" }));
      expect(
        screen.queryByRole("button", { name: "保存资源更改" }),
      ).not.toBeInTheDocument();
      expect(createVersion).not.toHaveBeenCalled();
    },
  );

  it("shows the resource tree returned by the current version API", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await screen.findByRole("radio", { name: "资源文件 3" });

    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));
    expect(
      screen.getByRole("button", { name: "references" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "checklist.md" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "checklist.md" }));
    expect(
      await screen.findByText("- 检查当前实现", { exact: false }),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", {
        name: "编辑 references/checklist.md",
      }),
    );
    const editor = screen.getByRole("textbox", {
      name: "编辑 references/checklist.md",
    });
    await user.clear(editor);
    await user.type(editor, "new checklist");
    await user.click(screen.getByRole("button", { name: "应用文件更改" }));
    await user.click(screen.getByRole("button", { name: "保存资源更改" }));

    await waitFor(() => expect(createVersion).toHaveBeenCalledTimes(1));
    expect(createVersion.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          files: [],
          resourceOperations: [
            {
              action: "write",
              path: "references/checklist.md",
              contentBase64: "bmV3IGNoZWNrbGlzdA==",
            },
          ],
          version: "1.4.1",
        }),
      }),
    );
  });

  it("在本地联动资源操作并确认后只创建一次版本", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));

    await user.click(screen.getByRole("button", { name: "references" }));
    await user.click(screen.getByRole("button", { name: "新建目录" }));
    let dialog = screen.getByRole("dialog", { name: "新建目录" });
    expect(
      within(dialog)
        .getByRole("button", { name: "创建目录" })
        .closest("footer"),
    ).not.toBeNull();
    const directoryPath = within(dialog).getByRole("textbox", {
      name: "资源路径",
    });
    await user.clear(directoryPath);
    await user.type(directoryPath, "references/examples");
    await user.click(within(dialog).getByRole("button", { name: "创建目录" }));

    await user.click(screen.getByRole("radio", { name: "SKILL.md" }));
    await user.click(screen.getByRole("radio", { name: "资源文件 3" }));
    expect(
      screen.getByRole("button", { name: "examples" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "新建文件" }));
    dialog = screen.getByRole("dialog", { name: "新建文件" });
    const filePath = within(dialog).getByRole("textbox", {
      name: "资源路径",
    });
    await user.clear(filePath);
    await user.type(filePath, "references/examples/guide.md");
    await user.click(within(dialog).getByRole("button", { name: "创建文件" }));

    await user.click(screen.getByRole("button", { name: "重命名或移动" }));
    dialog = screen.getByRole("dialog", { name: "重命名或移动" });
    const targetPath = within(dialog).getByRole("textbox", {
      name: "目标路径",
    });
    await user.clear(targetPath);
    await user.type(targetPath, "references/guide.md");
    await user.click(within(dialog).getByRole("button", { name: "确认移动" }));

    await user.upload(
      screen.getByLabelText("选择上传文件"),
      new File(["uploaded guide"], "ignored.md", { type: "text/markdown" }),
    );

    await user.click(screen.getByRole("button", { name: "checklist.md" }));
    await user.click(screen.getByRole("button", { name: "删除资源" }));
    dialog = screen.getByRole("dialog", { name: "删除资源" });
    await user.click(within(dialog).getByRole("button", { name: "确认删除" }));

    expect(createVersion).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "保存资源更改" }));

    await waitFor(() => expect(createVersion).toHaveBeenCalledTimes(1));
    expect(createVersion.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          files: [],
          version: "1.4.1",
          resourceOperations: [
            { action: "mkdir", path: "references/examples" },
            {
              action: "write",
              path: "references/examples/guide.md",
              contentBase64: "",
            },
            {
              action: "move",
              path: "references/examples/guide.md",
              targetPath: "references/guide.md",
            },
            {
              action: "write",
              path: "references/guide.md",
              contentBase64: "dXBsb2FkZWQgZ3VpZGU=",
            },
            { action: "delete", path: "references/checklist.md" },
          ],
        }),
      }),
    );
  });

  it("保留 scripts 文件执行位并在保存时一并提交", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));
    await user.click(screen.getByRole("button", { name: "scripts" }));
    await user.click(screen.getByRole("button", { name: "review.sh" }));
    await screen.findByText("- 检查当前实现", { exact: false });

    await user.click(
      screen.getByRole("button", { name: "编辑 scripts/review.sh" }),
    );
    const editor = screen.getByRole("textbox", {
      name: "编辑 scripts/review.sh",
    });
    await user.clear(editor);
    await user.type(editor, "#!/bin/sh\necho ok");
    await user.click(screen.getByRole("button", { name: "应用文件更改" }));
    await user.click(screen.getByRole("button", { name: "保存资源更改" }));

    await waitFor(() => expect(createVersion).toHaveBeenCalledTimes(1));
    expect(createVersion.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          resourceOperations: [
            expect.objectContaining({
              action: "write",
              path: "scripts/review.sh",
              executable: true,
            }),
          ],
        }),
      }),
    );
  });

  it("在读取前拒绝超大上传并阻止普通文件作为父目录", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));
    await user.click(screen.getByRole("button", { name: "references" }));

    const oversized = new File(["small"], "large.bin");
    Object.defineProperty(oversized, "size", { value: 50 * 1024 * 1024 + 1 });
    await user.upload(screen.getByLabelText("选择上传文件"), oversized);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "单个文件不能超过 50 MiB。",
    );

    await user.click(screen.getByRole("button", { name: "新建文件" }));
    let dialog = screen.getByRole("dialog", { name: "新建文件" });
    let input = within(dialog).getByRole("textbox", { name: "资源路径" });
    await user.clear(input);
    await user.type(input, "references/block");
    await user.click(within(dialog).getByRole("button", { name: "创建文件" }));

    await user.click(screen.getByRole("button", { name: "新建文件" }));
    dialog = screen.getByRole("dialog", { name: "新建文件" });
    input = within(dialog).getByRole("textbox", { name: "资源路径" });
    await user.clear(input);
    await user.type(input, "references/block/child.md");
    expect(within(dialog).getByRole("alert")).toHaveTextContent(
      "请输入未被占用的有效相对路径",
    );
    expect(
      within(dialog).getByRole("button", { name: "创建文件" }),
    ).toBeDisabled();
  });

  it("下载当前资源文件", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(await screen.findByRole("radio", { name: "资源文件 3" }));
    await user.click(screen.getByRole("button", { name: "checklist.md" }));
    await screen.findByText("- 检查当前实现", { exact: false });

    await user.click(screen.getByRole("button", { name: "下载资源" }));

    expect(saveBlob).toHaveBeenCalledWith(expect.any(Blob), "checklist.md");
  });

  it("saves name and category together without creating a version", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    await user.click(screen.getByRole("button", { name: "编辑技能" }));
    const name = screen.getByRole("textbox", { name: "展示名称" });
    await user.clear(name);
    await user.type(name, "评审助手");
    const category = screen.getByRole("combobox", { name: "分类" });
    await user.click(screen.getByRole("button", { name: "移除分类“开发”" }));
    await user.type(category, "写作{Enter}");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updateSkill).toHaveBeenCalledTimes(1));
    expect(updateSkill.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "code-review" },
        body: { name: "评审助手", categories: ["写作"] },
      }),
    );
    expect(createVersion).not.toHaveBeenCalled();
  });

  it.each(["写作", ""])(
    "updates or clears only the skill category (%s)",
    async (category) => {
      const user = userEvent.setup();
      renderPage();
      await user.click(await screen.findByRole("button", { name: "编辑技能" }));
      const dialog = screen.getByRole("dialog", { name: "编辑技能" });
      const input = within(dialog).getByRole("combobox", { name: "分类" });
      expect(
        within(dialog).getByRole("button", { name: "移除分类“开发”" }),
      ).toBeInTheDocument();
      await user.click(
        within(dialog).getByRole("button", { name: "移除分类“开发”" }),
      );
      if (category) await user.type(input, `${category}{Enter}`);
      await user.click(within(dialog).getByRole("button", { name: "保存" }));
      await waitFor(() => expect(updateSkill).toHaveBeenCalledTimes(1));
      expect(updateSkill.mock.calls[0]?.[0]).toMatchObject({
        path: { wakerId: "waker-1", skillId: "code-review" },
        body: { name: "代码审查", categories: category ? [category] : [] },
      });
      expect(createVersion).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );
    },
  );

  it("edits a library category without claiming partner enable states from its identity preview", async () => {
    const user = userEvent.setup();
    renderPage("/resources/skills/code-review");
    await screen.findByRole("heading", { name: "代码伙伴" });
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "编辑技能" }));
    const dialog = screen.getByRole("dialog", { name: "编辑技能" });
    await user.click(
      within(dialog).getByRole("button", { name: "移除分类“开发”" }),
    );
    await user.type(
      within(dialog).getByRole("combobox", { name: "分类" }),
      "资料整理{Enter}",
    );
    await user.click(within(dialog).getByRole("button", { name: "保存" }));
    await waitFor(() => expect(updateSkill).toHaveBeenCalledTimes(1));
    expect(updateSkill.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "", skillId: "code-review" },
      body: { name: "代码审查", categories: ["资料整理"] },
    });
    expect(createVersion).not.toHaveBeenCalled();
  });

  it("shows metadata as read-only fields", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    expect(screen.getByText("信息")).toBeInTheDocument();
    expect(screen.getByText("开发")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "修改分类" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("code-review")).not.toBeInTheDocument();
    expect(screen.getByText("Review real changes")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "编辑元数据" }),
    ).not.toBeInTheDocument();
  });

  it("shows changed file names in version diff and restores with the suggested version", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(screen.getByRole("radio", { name: "版本" }));
    await user.click(
      screen.getByRole("button", { name: "查看 v1.3.0 与当前版本的差异" }),
    );

    expect(
      await screen.findByText("references/checklist.md"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "应用后的新版本号" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "恢复为 v1.4.1" }));

    await waitFor(() => expect(restoreVersion).toHaveBeenCalledTimes(1));
    expect(restoreVersion.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ body: { notes: "" } }),
    );
  });

  it("暗色主题下使用暗色版本差异配色", async () => {
    document.documentElement.dataset.theme = "dark";
    themeState.resolvedTheme = "dark";
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(screen.getByRole("radio", { name: "版本" }));
    await user.click(
      screen.getByRole("button", { name: "查看 v1.3.0 与当前版本的差异" }),
    );

    expect(await screen.findByTestId("skill-diff")).toHaveAttribute(
      "data-theme",
      "dark",
    );
  });

  it("uninstalls a user skill from its partner row and returns to the list", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    await user.click(screen.getByRole("button", { name: "移除" }));
    const dialog = screen.getByRole("dialog", { name: "移除技能" });
    await user.click(within(dialog).getByRole("button", { name: "确认移除" }));

    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(1));
    expect(uninstallSkill.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "code-review" },
      }),
    );
    expect(await screen.findByText("技能列表页")).toBeInTheDocument();
  });

  it("removes a fifth installed partner from the global page without deleting the skill", async () => {
    pageFixture.partners = Array.from({ length: 5 }, (_, index) => ({
      id: `waker-${index + 1}`,
      name: `伙伴${index + 1}`,
    }));
    const user = userEvent.setup();
    renderPage("/resources/skills/code-review");
    const partner = await screen.findByRole("heading", { name: "伙伴5" });
    const row = partner.parentElement?.parentElement;
    if (!row) throw new Error("Partner row is missing");
    await user.click(within(row).getByRole("button", { name: "移除" }));
    const dialog = screen.getByRole("dialog", { name: "移除技能" });
    expect(dialog).toHaveTextContent(
      "将从“伙伴5”移除技能“代码审查”，其他伙伴不受影响。",
    );
    expect(dialog).toHaveTextContent("技能库和版本会保留");
    expect(uninstallSkill).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "确认移除" }));
    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(1));
    expect(uninstallSkill.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "waker-5", skillId: "code-review" },
      query: undefined,
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "代码审查" }),
    ).toBeInTheDocument();
    expect(setEnabled).not.toHaveBeenCalled();
  });

  it("confirms global deletion with the complete affected partner count", async () => {
    pageFixture.partners = Array.from({ length: 5 }, (_, index) => ({
      id: `waker-${index + 1}`,
      name: `伙伴${index + 1}`,
    }));
    const user = userEvent.setup();
    renderPage("/resources/skills/code-review");
    const remove = await screen.findByRole("button", { name: "删除技能" });
    const dangerSection = remove.closest("section");
    expect(
      screen.getAllByRole("heading", { level: 2 }).at(-1),
    ).toHaveTextContent("危险操作");
    expect(dangerSection).toHaveTextContent(
      "将删除此技能的全部版本，并移除全部 5 个伙伴的安装关系。",
    );
    expect(
      screen.getByRole("heading", { name: "代码审查" }).closest("header"),
    ).not.toContainElement(remove);
    await user.click(remove);
    const dialog = screen.getByRole("dialog", { name: "删除技能" });
    expect(dialog).toHaveTextContent("并移除全部 5 个伙伴的安装关系");
    await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(1));
    expect(uninstallSkill.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "", skillId: "code-review" },
      query: { confirmed: true, expectedAffectedCount: 5 },
    });
  });

  it("removes a partner relation while preserving the library resource", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "移除" }));
    expect(
      screen.queryByRole("button", { name: "删除技能" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "危险操作" }),
    ).not.toBeInTheDocument();
    const dialog = screen.getByRole("dialog", { name: "移除技能" });
    expect(dialog).toHaveTextContent(
      "仅移除伙伴的安装关系。技能库和版本会保留。",
    );
    await user.click(within(dialog).getByRole("button", { name: "确认移除" }));
    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledOnce());
    expect(uninstallSkill.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "waker-1", skillId: "code-review" },
    });
  });

  it("启停失败时在详情操作区就近显示错误", async () => {
    setEnabled.mockRejectedValueOnce(new Error("request failed"));
    const user = userEvent.setup();
    renderPage();

    await user.click(
      await screen.findByRole("switch", { name: "停用 代码审查" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "技能启停失败，请稍后重试。",
    );
  });

  it("Git 拉取弹窗使用固定在底部的共享操作区", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "更新" }));

    const dialog = screen.getByRole("dialog", { name: "拉取更新" });
    const footer = within(dialog)
      .getByRole("button", { name: "取消" })
      .closest("footer");
    expect(footer).not.toBeNull();
    expect(footer?.parentElement).toBe(dialog);
    expect(footer).toHaveClass("mt-auto", "justify-end", "shrink-0");
  });
});
