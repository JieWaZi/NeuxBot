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
    queryFn: async () => ({
      skill: {
        id: "code-review",
        name: "代码审查",
        description: "检查真实改动",
        scope: "public",
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
    }),
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
  themeState.resolvedTheme = "light";
  delete document.documentElement.dataset.theme;
  createVersion.mockClear();
  pullUpdate.mockClear();
  restoreVersion.mockClear();
  saveBlob.mockClear();
  setEnabled.mockReset();
  setEnabled.mockResolvedValue({ success: true });
  uninstallSkill.mockClear();
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/wakers/waker-1/skills/code-review"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/skills/:skillId"
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
  it("uses the NeuxBot detail layout with a title return action and exposes real content tabs", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "代码审查" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "代码审查" }).closest("header"),
    ).not.toHaveClass("border-b");
    expect(screen.getByText("code-review")).toBeInTheDocument();
    expect(screen.getByText("Review real changes")).toBeInTheDocument();
    await screen.findByText(
      "检查真实改动。",
      { exact: true },
      { timeout: 5000 },
    );
    expect(
      screen.getByRole("button", { name: "返回技能列表" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "说明" })).toBeChecked();
    expect(
      screen.getByRole("radio", { name: "资源文件 3" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: "当前版本 v1.4.0" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: "当前版本 v1.4.0" }));
    expect(
      screen.getByRole("heading", { name: "版本历史" }),
    ).toBeInTheDocument();
  });

  it("从标题区返回当前伙伴的技能列表", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.hover(screen.getByRole("button", { name: "返回技能列表" }));
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

    await user.click(screen.getByRole("radio", { name: "说明" }));
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

  it("updates display name without creating a version", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    await user.click(screen.getByRole("button", { name: "编辑展示名称" }));
    const name = screen.getByRole("textbox", { name: "展示名称" });
    await user.clear(name);
    await user.type(name, "评审助手");
    await user.click(screen.getByRole("button", { name: "保存名称" }));

    await waitFor(() => expect(updateSkill).toHaveBeenCalledTimes(1));
    expect(updateSkill.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "code-review" },
        body: { name: "评审助手" },
      }),
    );
  });

  it("shows metadata as read-only fields", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    expect(screen.getByText("元数据")).toBeInTheDocument();
    expect(screen.getByText("code-review")).toBeInTheDocument();
    expect(screen.getByText("Review real changes")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "编辑元数据" }),
    ).not.toBeInTheDocument();
  });

  it("shows changed file names in version diff and restores with the suggested version", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });
    await user.click(screen.getByRole("radio", { name: "当前版本 v1.4.0" }));
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
    await user.click(screen.getByRole("radio", { name: "当前版本 v1.4.0" }));
    await user.click(
      screen.getByRole("button", { name: "查看 v1.3.0 与当前版本的差异" }),
    );

    expect(await screen.findByTestId("skill-diff")).toHaveAttribute(
      "data-theme",
      "dark",
    );
  });

  it("uninstalls a user skill from the detail header and returns to the list", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "代码审查" });

    await user.click(screen.getByRole("button", { name: "卸载技能" }));
    const dialog = screen.getByRole("dialog", { name: "卸载技能" });
    await user.click(within(dialog).getByRole("button", { name: "确认卸载" }));

    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(1));
    expect(uninstallSkill.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "code-review" },
      }),
    );
    expect(await screen.findByText("技能列表页")).toBeInTheDocument();
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
