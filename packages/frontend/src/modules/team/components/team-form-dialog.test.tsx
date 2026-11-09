import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
import type * as TeamSDK from "@/shared/api/generated/sdk.gen";

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
});
afterAll(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
  Reflect.deleteProperty(Element.prototype, "setPointerCapture");
  Reflect.deleteProperty(Element.prototype, "releasePointerCapture");
});

const fixtures = vi.hoisted(() => ({
  update: vi.fn(),
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
    queryFn: async () => ({
      resources: [
        {
          id: "managed",
          name: "测试账号",
          runtimeId: "codex",
          hasCredential: true,
          authMode: "api-key",
          endpoint: { protocol: "openai-responses" },
          selectedModels: ["gpt-test"],
        },
      ],
    }),
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
          {
            id: "claude",
            name: "Claude",
            enabled: true,
            baseRuntimeId: "claude",
            state: "ready",
          },
        ],
      };
    },
  }),
}));

vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof TeamSDK>()),
  getRuntimeModelCatalog: async ({
    path,
    query,
  }: {
    path: { runtimeId: string };
    query: { accountId: string };
  }) => ({
    data: {
      runtimeId: path.runtimeId,
      accountId: query.accountId,
      state: "fresh",
      models: [
        {
          id: "gpt-test",
          name: "原模型",
          reasoningOptions: [{ id: "medium", name: "中" }],
          permissionLevels: ["default", "auto", "full-access"],
        },
        {
          id: "next-model",
          name: "另一模型",
          permissionLevels: ["default", "auto", "full-access"],
        },
      ],
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
  updateChannelMutation: () => ({ mutationFn: fixtures.update }),
  getChannelQueryKey: () => ["channel"],
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

beforeEach(() => {
  fixtures.runtimesError = false;
  fixtures.wakersError = false;
});

describe("TeamFormDialog", () => {
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

it.each([true, false, undefined])("编辑团队保留成员 Fast=%s", async (fast) => {
  const user = userEvent.setup();
  fixtures.update.mockReset().mockResolvedValue({});
  const detail = {
    channel: { id: "team", name: "原团队", leadWakerId: "waker-1" },
    members: ["waker-1", "waker-2"].map((id) => ({
      waker: { id, name: id },
      runtimeId: "codex",
      accountId: "system",
      model: "gpt-test",
      reasoning: "medium",
      permissionLevel: "auto" as const,
      fast,
    })),
  };
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <TeamFormDialog open onOpenChange={vi.fn()} detail={detail} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const name = await screen.findByLabelText(/团队名称/);
  await user.clear(name);
  await user.type(name, "新团队名称");
  await user.click(screen.getByRole("button", { name: "保存团队" }));
  await waitFor(() => expect(fixtures.update).toHaveBeenCalledTimes(1));
  expect(
    fixtures.update.mock.calls[0]?.[0].body.members.map(
      (member: { fast?: boolean }) => member.fast,
    ),
  ).toEqual([fast, fast]);
});

it.each(
  [true, false].flatMap((fast) =>
    (["runtime", "account", "model"] as const).map((kind) => ({ fast, kind })),
  ),
)("团队改选 $kind 清除 Fast=$fast 并保留其他成员", async ({ fast, kind }) => {
  const user = userEvent.setup();
  fixtures.update.mockReset().mockResolvedValue({});
  const detail = {
    channel: { id: "team", name: "原团队", leadWakerId: "waker-1" },
    members: ["waker-1", "waker-2"].map((id) => ({
      waker: { id, name: id },
      runtimeId: "codex",
      accountId: "system",
      model: "gpt-test",
      reasoning: "medium",
      permissionLevel: "auto" as const,
      fast,
    })),
  };
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <TeamFormDialog open onOpenChange={vi.fn()} detail={detail} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const labels = { runtime: "智能体", account: "账号", model: "模型" };
  const control = await screen.findByRole("combobox", {
    name: `waker-1 · ${labels[kind]}`,
  });
  await user.click(control);
  await user.click(
    await screen.findByRole("option", {
      name: { runtime: /Claude/, account: "测试账号", model: "另一模型" }[kind],
    }),
  );
  await user.click(screen.getByRole("button", { name: "保存团队" }));
  await waitFor(() => expect(fixtures.update).toHaveBeenCalledTimes(1));
  expect(
    fixtures.update.mock.calls[0]?.[0].body.members.map(
      (member: { fast?: boolean }) => member.fast,
    ),
  ).toEqual([undefined, fast]);
});
