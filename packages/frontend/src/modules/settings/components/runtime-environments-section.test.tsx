import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { RuntimeEnvironmentsSection } from "./runtime-environments-section";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";

const { list, update, refresh, runtimeRows, modelCatalog, installationState } =
  vi.hoisted(() => ({
    installationState: {
      current: "idle",
      installed: undefined as boolean | undefined,
    },
    list: vi.fn(),
    update: vi.fn(async () => undefined),
    refresh: vi.fn(async () => undefined),
    runtimeRows: { current: [] as Array<Record<string, unknown>> },
    modelCatalog: vi.fn(),
  }));
vi.mock("@/shared/ui/scene-breadcrumbs", () => ({
  SceneBreadcrumbs: () => null,
}));
vi.mock("./ai-accounts-section", () => ({
  AIAccountsSection: ({
    onSelectAccount,
  }: {
    onSelectAccount: (id: string) => Promise<void>;
  }) => (
    <button onClick={() => void onSelectAccount("work")}>选择工作账号</button>
  ),
}));
vi.mock("./runtime-installation-controls", () => ({
  runtimeInstallationOptions: (runtimeId: string) => ({
    queryKey: ["installation", runtimeId],
    queryFn: async () => ({
      supported: true,
      state: installationState.current,
      installed: installationState.installed,
    }),
  }),
  RuntimeInstallationControls: () => null,
}));
vi.mock("./runtime-authentication-controls", () => ({
  runtimeAuthenticationOptions: (runtimeId: string) => ({
    queryKey: ["runtime-authentication", runtimeId],
    queryFn: async () => ({ state: "idle" }),
  }),
}));
vi.mock("./runtime-program-removal", () => ({
  RuntimeProgramRemoval: () => null,
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getRuntimeModelCatalog: modelCatalog,
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listRuntimesOptions: () => ({ queryKey: ["runtimes"], queryFn: list }),
  listRuntimesQueryKey: () => ["runtimes"],
  listAiAccountsOptions: () => ({
    queryKey: ["accounts"],
    queryFn: async () => ({ resources: [] }),
  }),
  updateRuntimeEnvironmentMutation: () => ({ mutationFn: update }),
  refreshRuntimesMutation: () => ({ mutationFn: refresh }),
  refreshRuntimeMutation: () => ({ mutationFn: refresh }),
  restartRuntimeMutation: () => ({ mutationFn: refresh }),
}));
beforeEach(() => {
  installationState.current = "idle";
  installationState.installed = undefined;
  update.mockClear();
  refresh.mockClear();
  runtimeRows.current = [
    {
      id: "codex",
      name: "Codex",
      installed: true,
      enabled: true,
      isDefault: true,
      binary: "codex",
      state: "ready",
      auth: "ok",
      version: "1.2.3",
      defaultModel: "model-a",
      defaultReasoning: "high",
      defaultPermissionLevel: "auto",
      models: [],
    },
    {
      id: "claude",
      name: "Claude",
      installed: false,
      enabled: false,
      binary: "claude",
      state: "not-installed",
      models: [],
    },
    {
      id: "kimi",
      name: "Kimi",
      installed: true,
      enabled: true,
      binary: "kimi",
      state: "not-ready",
      auth: "missing",
      models: [],
    },
  ];
  list
    .mockReset()
    .mockImplementation(async () => ({ resources: runtimeRows.current }));
  modelCatalog.mockReset().mockImplementation(async ({ path, query }) => ({
    data: {
      runtimeId: path.runtimeId,
      accountId: query.accountId,
      state: "fresh",
      defaultModel: "model-a",
      models: [
        {
          id: "model-a",
          name: "Model A",
          configurationKnown: true,
          permissionLevels: ["default", "auto", "full-access"],
          reasoningOptions: [{ id: "high", name: "High" }],
        },
        {
          id: "model-b",
          name: "Model B",
          configurationKnown: true,
          permissionLevels: ["default", "auto", "full-access"],
        },
      ],
    },
  }));
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.scrollIntoView = () => undefined;
});
function show(runtimeId?: string) {
  const onOpenRuntime = vi.fn();
  const onBack = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RuntimeEnvironmentsSection
          runtimeId={runtimeId}
          onOpenRuntime={onOpenRuntime}
          onBack={onBack}
          showSceneChrome={false}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { client, onOpenRuntime, onBack };
}
it("列表按真实安装事实分组，未登录的已安装智能体仍保留且整行进入详情", async () => {
  const user = userEvent.setup();
  const { onOpenRuntime } = show();
  await screen.findByRole("button", { name: "打开Kimi" });
  const installed = screen
    .getByRole("heading", { name: "已安装" })
    .closest("section");
  const other = screen
    .getByRole("heading", { name: "其他智能体" })
    .closest("section");
  expect(
    installed && within(installed).getByRole("button", { name: "打开Kimi" }),
  ).toBeVisible();
  expect(
    other && within(other).getByRole("button", { name: "打开Claude" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "打开Codex" }));
  expect(onOpenRuntime).toHaveBeenCalledWith("codex");
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
});
it("搜索同时筛选已安装和其他智能体", async () => {
  const user = userEvent.setup();
  show();
  await screen.findByRole("button", { name: "打开Codex" });
  await user.type(
    screen.getByRole("searchbox", { name: "搜索智能体" }),
    "Claude",
  );
  expect(screen.getByRole("button", { name: "打开Claude" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "打开Codex" }),
  ).not.toBeInTheDocument();
});
it("程序就绪但认证未知时列表不宣称已连接", async () => {
  runtimeRows.current[0] = { ...runtimeRows.current[0], auth: "unknown" };
  show();
  const row = await screen.findByRole("button", { name: "打开Codex" });
  expect(row).toHaveTextContent("可用");
  expect(row).not.toHaveTextContent("已连接");
});
it("详情改变默认模型即时保存且保留启用配置", async () => {
  const user = userEvent.setup();
  show("codex");
  const model = await screen.findByRole("combobox", { name: "默认模型" });
  // 后台目录即使暂时不返回，也不应延长已确认默认值写入的禁用时间。
  modelCatalog.mockImplementation(() => new Promise(() => {}));
  await user.click(model);
  await user.click(await screen.findByRole("option", { name: "Model B" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "codex" },
        body: expect.objectContaining({
          enabled: true,
          defaultModel: "model-b",
          defaultReasoning: "",
        }),
      }),
      expect.anything(),
    ),
  );
  await waitFor(() => expect(model).not.toBeDisabled());
});
it("切换详情账号清除旧账号模型默认值", async () => {
  const user = userEvent.setup();
  show("codex");
  await user.click(await screen.findByRole("button", { name: "选择工作账号" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          defaultAccountId: "work",
          defaultModel: "",
          defaultReasoning: "",
        }),
      }),
      expect.anything(),
    ),
  );
});

it("详情使用共享悬停返回操作回到智能体列表", async () => {
  const { onBack } = show("codex");
  await userEvent.click(
    await screen.findByRole("button", { name: "返回智能体" }),
  );
  expect(onBack).toHaveBeenCalledOnce();
});

it("后台读取失败保留已有智能体并允许重试", async () => {
  const { client } = show();
  await screen.findByRole("button", { name: "打开Codex" });
  list.mockRejectedValue(new Error("连接失败"));
  await client.invalidateQueries({ queryKey: ["runtimes"] });
  expect(await screen.findByRole("alert")).toHaveTextContent("连接失败");
  expect(screen.getByRole("button", { name: "打开Codex" })).toBeVisible();
  expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
});

it("Fast 缺省保留继承选项，选择关闭后保存显式值", async () => {
  modelCatalog.mockResolvedValue({
    data: {
      runtimeId: "codex",
      accountId: "system",
      state: "fresh",
      defaultModel: "model-a",
      models: [
        {
          id: "model-a",
          name: "Model A",
          configurationKnown: true,
          permissionLevels: ["auto"],
          fastSupported: true,
          defaultFast: true,
        },
      ],
    },
  });
  const user = userEvent.setup();
  show("codex");
  const fast = await screen.findByRole("combobox", { name: "Fast" });
  expect(fast).toHaveTextContent("默认");
  await user.click(fast);
  await user.click(await screen.findByRole("option", { name: "关闭" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({ defaultFast: false }),
      }),
      expect.anything(),
    ),
  );
});
it("详情首次请求失败仅显示读取错误和重试，不声称智能体不存在", async () => {
  list.mockRejectedValue(new Error("连接失败"));
  show("codex");
  expect(await screen.findByRole("alert")).toHaveTextContent("连接失败");
  expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
  expect(screen.queryByText("未找到此智能体。")).not.toBeInTheDocument();
});

it("详情启用和权限集中在名称右侧，不再重复用途和程序配置", async () => {
  show("codex");
  const header = (
    await screen.findByRole("heading", { name: "Codex" })
  ).closest("header");
  if (!header) throw new Error("缺少详情身份头");
  expect(
    within(header).getByRole("switch", { name: "启用智能体" }),
  ).toBeVisible();
  expect(
    within(header).getByRole("button", { name: "默认权限" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "用途" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "配置" }),
  ).not.toBeInTheDocument();
});
it("检测图标紧邻搜索且没有重复底部操作行", async () => {
  show();
  await screen.findByRole("button", { name: "打开Codex" });
  const check = screen.getByRole("button", { name: "重新检测" });
  const search = screen.getByRole("searchbox", { name: "搜索智能体" });
  expect(check.textContent).toBe("");
  expect(
    check.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(screen.getAllByRole("button", { name: "重新检测" })).toHaveLength(1);
});
it("模型目录不可用使用翻译文案，不显示原生诊断代码", async () => {
  runtimeRows.current[0] = {
    ...runtimeRows.current[0],
    diagnostics: [
      {
        code: "ACP_OPTIONS_UNAVAILABLE",
        level: "warning",
        message: "catalog_unavailable",
      },
    ],
  };
  modelCatalog.mockResolvedValue({
    data: {
      runtimeId: "codex",
      accountId: "system",
      state: "unavailable",
      errorCode: "catalog_unavailable",
      models: [],
    },
  });
  show("codex");
  expect(await screen.findByText("无法读取模型目录")).toBeVisible();
  expect(screen.queryByText("catalog_unavailable")).not.toBeInTheDocument();
  expect(screen.queryByText("agents.diagnostics")).not.toBeInTheDocument();
});

it("安装进行中禁用配置，详情不再重复重新检测", async () => {
  installationState.current = "installing";
  show("codex");
  await waitFor(() =>
    expect(screen.getByRole("switch", { name: "启用智能体" })).toBeDisabled(),
  );
  expect(screen.getByRole("button", { name: "默认权限" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "重新检测" }),
  ).not.toBeInTheDocument();
  expect(
    await screen.findByRole("combobox", { name: "默认模型" }),
  ).toBeDisabled();
});

it("重启使用单图标且仍按真实智能体提交", async () => {
  show("codex");
  const restart = await screen.findByRole("button", { name: "重启Codex" });
  expect(restart.textContent).toBe("");
  expect(restart.querySelector("svg")).toHaveClass("lucide-power");
  expect(restart).not.toHaveClass("border");
  await userEvent.click(restart);
  await waitFor(() =>
    expect(refresh).toHaveBeenCalledWith(
      expect.objectContaining({ path: { runtimeId: "codex" } }),
      expect.anything(),
    ),
  );
});

it("程序快照确认已删除时不沿用旧列表的连接状态和可编辑模型", async () => {
  installationState.installed = false;
  show("codex");
  await waitFor(() =>
    expect(
      screen.queryByRole("combobox", { name: "默认模型" }),
    ).not.toBeInTheDocument(),
  );
  expect(await screen.findByRole("heading", { name: "登录" })).toBeVisible();
  expect(screen.queryByText("已连接")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重启Codex" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "默认权限" })).toBeDisabled();
});
