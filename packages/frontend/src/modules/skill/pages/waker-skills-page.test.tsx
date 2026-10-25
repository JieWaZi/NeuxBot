import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerSkillsPage } from "./waker-skills-page";

const {
  importSkill,
  invalidateVisibleSkills,
  scanSkill,
  setEnabled,
  uninstallSkill,
} = vi.hoisted(() => ({
  importSkill: vi.fn<(options: Record<string, unknown>) => Promise<unknown>>(
    async () => [],
  ),
  invalidateVisibleSkills: vi.fn(async () => undefined),
  scanSkill: vi.fn<(options: Record<string, unknown>) => Promise<unknown>>(
    async () => ({ token: "batch-1", candidates: [] }),
  ),
  setEnabled: vi.fn<
    (options: Record<string, unknown>) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
  uninstallSkill: vi.fn<
    (options: Record<string, unknown>) => Promise<{ success: boolean }>
  >(async () => ({ success: true })),
}));

vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("../api/skill-queries", () => ({
  invalidateVisibleSkillQueries: invalidateVisibleSkills,
  listWakerSkillsOptions: () => ({
    queryKey: ["waker-skills", "waker-1"],
    queryFn: async () => [
      {
        enabled: true,
        skill: {
          id: "skill-private",
          name: "私有技能",
          description: "当前伙伴的私有能力",
          scope: "private",
          source: "zip",
          updatedAt: 1,
        },
        currentVersion: { id: "version-private", version: "1.1.0" },
      },
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
          scope: "public",
          source: "git",
          updatedAt: 1,
        },
        currentVersion: { id: "version-1", version: "1.4.0" },
      },
      {
        enabled: true,
        skill: {
          id: "skill-builtin",
          name: "内置技能",
          description: "NeuxBot 内置能力",
          scope: "public",
          source: "builtin",
          builtIn: true,
          updatedAt: 1,
        },
        currentVersion: { id: "version-2", version: "1.0.0" },
      },
    ],
  }),
  listWakerSkillsQueryKey: () => ["waker-skills", "waker-1"],
  setWakerSkillEnabledMutation: () => ({ mutationFn: setEnabled }),
  uninstallSkillMutation: () => ({ mutationFn: uninstallSkill }),
  importWakerSkillMutation: () => ({
    mutationFn: importSkill,
  }),
  scanWakerSkillImportMutation: () => ({
    mutationFn: scanSkill,
  }),
}));

vi.mock("@/shared/platform", () => ({
  selectFiles: async () => [],
}));

beforeEach(() => {
  importSkill.mockReset();
  importSkill.mockResolvedValue([]);
  invalidateVisibleSkills.mockClear();
  scanSkill.mockReset();
  scanSkill.mockResolvedValue({ token: "batch-1", candidates: [] });
  setEnabled.mockClear();
  uninstallSkill.mockClear();
});

function LocationSearch() {
  return <output aria-label="当前查询参数">{useLocation().search}</output>;
}

describe("WakerSkillsPage", () => {
  it("renders repository skills and persists enable changes", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("代码审查")).toBeInTheDocument();
    expect(screen.getByText("v1.4.0")).toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: "停用 代码审查" }));

    await waitFor(() => expect(setEnabled).toHaveBeenCalledTimes(1));
    expect(invalidateVisibleSkills).toHaveBeenCalledTimes(1);
    expect(setEnabled.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1", skillId: "skill-review" },
        body: { enabled: false },
      }),
    );
  });

  it("从 URL 恢复筛选并在搜索变化时更新 URL", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[
            "/wakers/waker-1/skills?search=代码&status=enabled&scope=public",
          ]}
        >
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={
                <>
                  <WakerSkillsPage />
                  <LocationSearch />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    const search = await screen.findByRole("searchbox", { name: "搜索技能" });
    expect(search).toHaveValue("代码");
    expect(await screen.findByText("代码审查")).toBeInTheDocument();
    expect(screen.queryByText("私有技能")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "技能范围" })).toBeVisible();

    await user.clear(search);
    await user.type(search, "内置");
    const currentSearch = screen.getByLabelText("当前查询参数").textContent;
    const currentParams = new URLSearchParams(currentSearch ?? "");
    expect(currentParams.get("search")).toBe("内置");
    expect(currentParams.get("status")).toBe("enabled");
    expect(currentParams.get("scope")).toBe("public");
  });

  it("启停失败时在技能列表就近显示错误", async () => {
    setEnabled.mockRejectedValueOnce(new Error("request failed"));
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await user.click(
      await screen.findByRole("switch", { name: "停用 代码审查" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "技能启停失败，请稍后重试。",
    );
  });

  it("扫描候选后只安装用户明确选择的技能", async () => {
    scanSkill.mockResolvedValueOnce({
      token: "batch-1",
      candidates: [
        {
          id: "frontend-review",
          name: "前端审查",
          description: "检查前端改动",
          installed: false,
        },
        {
          id: "backend-review",
          name: "后端审查",
          description: "检查后端改动",
          installed: false,
        },
        {
          id: "installed-review",
          name: "已安装审查",
          description: "已经安装",
          installed: true,
          installedSkillId: "skill-review",
        },
      ],
    });
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("技能")).toBeInTheDocument();
    expect(screen.queryByText("伙伴能力")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "新建" }));
    const dialog = screen.getByRole("dialog", { name: "添加技能" });
    expect(dialog).toHaveClass(
      "max-w-[720px]",
      "h-[min(800px,calc(100dvh-48px))]",
    );
    const sourceGroup = screen.getByRole("radiogroup", { name: "技能来源" });
    expect(sourceGroup).toHaveClass("grid", "w-full", "grid-cols-2");
    expect(screen.getByRole("radio", { name: /从 Git 导入/ })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("radio", { name: /上传 ZIP/ })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.queryByText("分支或标签")).not.toBeInTheDocument();
    expect(screen.queryByText("仓库子目录")).not.toBeInTheDocument();
    const dialogFooter = screen
      .getByRole("button", { name: "取消" })
      .closest("footer");
    expect(dialogFooter).not.toBeNull();
    expect(dialogFooter?.parentElement).toBe(dialog);
    expect(dialogFooter).toHaveClass("mt-auto", "justify-end", "shrink-0");
    await user.type(
      screen.getByRole("textbox", { name: "Git 地址" }),
      "https://github.com/example/skills/tree/main/catalog",
    );
    await user.click(screen.getByRole("button", { name: "扫描并继续" }));

    await screen.findByText("前端审查");
    expect(screen.getByText("已安装审查")).toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "选择候选 已安装审查" }),
    ).toBeDisabled();
    expect(screen.getByRole("link", { name: "前往更新" })).toHaveAttribute(
      "href",
      "/wakers/waker-1/skills/skill-review",
    );

    expect(
      screen.queryByRole("switch", { name: "全部候选的安装范围" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("checkbox", { name: "选择候选 后端审查" }),
    );
    await user.click(screen.getByRole("button", { name: "安装所选 1 个技能" }));

    await waitFor(() => expect(importSkill).toHaveBeenCalledTimes(1));
    expect(importSkill.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { wakerId: "waker-1" },
        body: {
          token: "batch-1",
          candidates: [
            {
              id: "frontend-review",
              version: "1.0.0",
              description: "检查前端改动",
            },
          ],
        },
      }),
    );
  });

  it("扫描不到候选时展示明确空状态并允许更换来源", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findByText("代码审查");
    await user.click(screen.getByRole("button", { name: "新建" }));
    await user.type(
      screen.getByRole("textbox", { name: "Git 地址" }),
      "https://github.com/example/empty.git",
    );
    await user.click(screen.getByRole("button", { name: "扫描并继续" }));

    expect(await screen.findByText("没有发现技能候选")).toBeVisible();
    expect(screen.getByRole("button", { name: "更换来源" })).toBeEnabled();
  });

  it("只选择可卸载技能并支持批量卸载", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findByText("内置技能");
    expect(
      screen.queryByRole("checkbox", { name: "选择技能 内置技能" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "卸载技能 内置技能" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "选择" }));
    expect(screen.getByText("已选择 2 项")).toBeInTheDocument();
    expect(screen.queryByText("技能市场")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "删除所选" }));

    const dialog = screen.getByRole("dialog", { name: "卸载所选技能" });
    await user.click(within(dialog).getByRole("button", { name: "卸载所选" }));

    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledTimes(2));
    expect(uninstallSkill.mock.calls.map(([options]) => options)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: { wakerId: "waker-1", skillId: "skill-review" },
        }),
        expect.objectContaining({
          path: { wakerId: "waker-1", skillId: "skill-private" },
        }),
      ]),
    );
  });
});
