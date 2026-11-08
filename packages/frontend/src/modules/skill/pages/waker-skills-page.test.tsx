import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerSkillsPage } from "./waker-skills-page";

const { invalidateVisibleSkills, setEnabled, uninstallSkill } = vi.hoisted(
  () => ({
    invalidateVisibleSkills: vi.fn(async () => undefined),
    setEnabled: vi.fn<
      (options: Record<string, unknown>) => Promise<{ success: boolean }>
    >(async () => ({ success: true })),
    uninstallSkill: vi.fn<
      (options: Record<string, unknown>) => Promise<{ success: boolean }>
    >(async () => ({ success: true })),
  }),
);

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
          id: "skill-writing",
          name: "技术写作",
          description: "文档写作能力",
          source: "zip",
          updatedAt: 1,
        },
        currentVersion: { id: "version-writing", version: "1.1.0" },
      },
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
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
}));

vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));

vi.mock("@/shared/platform", () => ({
  selectFiles: async () => [],
}));

beforeEach(() => {
  invalidateVisibleSkills.mockClear();
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
    expect(screen.getByText("2 个技能")).toBeVisible();
    expect(
      screen
        .getByRole("link", { name: "查看技能 代码审查" })
        .closest("article"),
    ).toHaveAttribute("data-appearance", "row");
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
        <MemoryRouter initialEntries={["/wakers/waker-1/skills?search=代码"]}>
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
    expect(screen.getByText("1 个技能")).toBeVisible();
    expect(screen.queryByText("技术写作")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();

    await user.clear(search);
    await user.type(search, "内置");
    const currentSearch = screen.getByLabelText("当前查询参数").textContent;
    const currentParams = new URLSearchParams(currentSearch ?? "");
    expect(currentParams.get("search")).toBe("内置");
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

  it("添加技能进入独立页并保留伙伴列表的搜索", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={["/wakers/waker-1/skills?search=代码"]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/skills"
              element={<WakerSkillsPage />}
            />
            <Route
              path="/wakers/:wakerId/skills/new"
              element={<LocationSearch />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "添加技能" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const params = new URLSearchParams(
      screen.getByLabelText("当前查询参数").textContent ?? "",
    );
    expect(params.get("returnTo")).toBe("/wakers/waker-1/skills?search=代码");
  });

  it("目录隐藏内部内置技能，不展示批量选择和列表卸载操作", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
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
    await screen.findByRole("link", { name: "查看技能 代码审查" });
    expect(
      screen.queryByRole("link", { name: "查看技能 内置技能" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /删除所选|卸载技能/ }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("已安装")).not.toBeInTheDocument();
    expect(uninstallSkill).not.toHaveBeenCalled();
  });
});
