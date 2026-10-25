import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import "@/shared/i18n";

import { TeamFormDialog } from "./team-form-dialog";

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeAll(() => vi.stubGlobal("ResizeObserver", ResizeObserverStub));
afterAll(() => vi.unstubAllGlobals());

const fixtures = vi.hoisted(() => ({
  runtimesError: false,
  wakersError: false,
  wakers: [
    { id: "waker-1", name: "伙伴一", roleName: "开发" },
    { id: "waker-2", name: "伙伴二", roleName: "测试" },
    { id: "waker-3", name: "伙伴三", roleName: "设计" },
  ],
}));

vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({
    queryKey: ["ai-accounts"],
    queryFn: async () => ({ resources: [] }),
  }),
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => {
      if (fixtures.runtimesError) throw new Error("runtimes unavailable");
      return {
        resources: [
          {
            defaultModel: "gpt-test",
            defaultPermissionLevel: "auto",
            defaultReasoning: "medium",
            enabled: true,
            id: "codex",
            isDefault: true,
            models: [
              {
                id: "gpt-test",
                reasoningOptions: [{ id: "medium", name: "中" }],
                permissionLevels: ["default", "auto", "full-access"],
              },
            ],
            name: "Codex",
            state: "ready",
          },
        ],
      };
    },
  }),
}));

vi.mock("../api/channel-queries", () => ({
  createChannelMutation: () => ({ mutationFn: vi.fn() }),
  listChannelsQueryKey: () => ["channels"],
  teamWakersInfiniteOptions: (query: string) => ({
    queryKey: ["wakers", query],
    initialPageParam: 1,
    queryFn: async () => {
      if (fixtures.wakersError) throw new Error("wakers unavailable");
      return {
        pageNum: 1,
        pageSize: 30,
        resources: fixtures.wakers,
        totalSize: fixtures.wakers.length,
      };
    },
    getNextPageParam: () => undefined,
  }),
  updateChannelMutation: () => ({ mutationFn: vi.fn() }),
  teamQueryKeys: { channels: () => ["channels"] },
}));

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <TeamFormDialog open onOpenChange={vi.fn()} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function selectMember(name: string) {
  fireEvent.click(screen.getByRole("checkbox", { name: new RegExp(name) }));
}

describe("TeamFormDialog", () => {
  beforeEach(() => {
    fixtures.runtimesError = false;
    fixtures.wakersError = false;
  });

  it("添加成员后仍要求用户显式选择唯一负责人", async () => {
    renderDialog();

    await screen.findByText("伙伴一");
    selectMember("伙伴一");
    selectMember("伙伴二");

    expect(screen.getByText("请选择团队负责人。")).toBeVisible();
    expect(
      screen.getAllByRole("button", { name: "设为团队负责人" }),
    ).toHaveLength(2);
    expect(screen.getByRole("button", { name: "创建团队" })).toBeDisabled();

    const firstLeadButton = screen.getAllByRole("button", {
      name: "设为团队负责人",
    })[0];
    expect(firstLeadButton).toBeDefined();
    if (!firstLeadButton) throw new Error("缺少负责人选择按钮");
    fireEvent.click(firstLeadButton);
    expect(screen.getByRole("button", { name: "团队负责人" })).toBeVisible();
    expect(screen.queryByText("请选择团队负责人。")).not.toBeInTheDocument();
  });

  it("移除当前负责人后保持负责人为空，不按成员顺序递补", async () => {
    renderDialog();

    await screen.findByText("伙伴一");
    selectMember("伙伴一");
    selectMember("伙伴二");
    selectMember("伙伴三");
    const firstLeadButton = screen.getAllByRole("button", {
      name: "设为团队负责人",
    })[0];
    expect(firstLeadButton).toBeDefined();
    if (!firstLeadButton) throw new Error("缺少负责人选择按钮");
    fireEvent.click(firstLeadButton);
    fireEvent.click(screen.getByRole("button", { name: "移除伙伴一" }));

    expect(screen.getByText("请选择团队负责人。")).toBeVisible();
    expect(
      screen.getAllByRole("button", { name: "设为团队负责人" }),
    ).toHaveLength(2);
    expect(
      screen.queryByRole("button", { name: "团队负责人" }),
    ).not.toBeInTheDocument();
  });

  it("为每位成员展示带可见标签的执行配置，并使用标准图标按钮", async () => {
    renderDialog();

    await screen.findByText("伙伴一");
    selectMember("伙伴一");
    selectMember("伙伴二");

    for (const label of ["智能体", "账号", "模型", "推理强度", "执行权限"]) {
      expect(screen.getAllByText(label)).toHaveLength(2);
    }
    for (const remove of screen.getAllByRole("button", { name: /移除伙伴/ })) {
      expect(remove).toHaveClass("size-control-sm");
    }
  });

  it("成员或运行时查询失败时展示就近错误和重试入口", async () => {
    fixtures.runtimesError = true;
    fixtures.wakersError = true;
    renderDialog();

    expect(await screen.findByText("伙伴列表暂时无法载入。")).toBeVisible();
    expect(screen.getByText("智能体暂时无法载入。")).toBeVisible();
    expect(screen.getAllByRole("button", { name: "重新载入" })).toHaveLength(2);
  });
});
