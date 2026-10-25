import { useQueryEvents } from "@/shared/api/use-query-events";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RuntimeEnvironmentsSection } from "./runtime-environments-section";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";

const {
  accounts,
  nativeModels,
  refreshAll,
  refreshOne,
  runtimes,
  thinking,
  updateRuntime,
} = vi.hoisted(() => ({
  accounts: { current: [] as Array<Record<string, unknown>> },
  nativeModels: vi.fn(),
  refreshAll: vi.fn(async () => undefined),
  refreshOne: vi.fn(async () => undefined),
  runtimes: { current: [] as Array<Record<string, unknown>> },
  thinking: vi.fn(),
  updateRuntime: vi.fn(async () => undefined),
}));

vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getAiAccountNativeModels: nativeModels,
  getAiAccountThinking: thinking,
}));

vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({
    queryKey: ["ai-accounts"],
    queryFn: async () => ({ resources: accounts.current }),
  }),
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => ({ resources: runtimes.current }),
  }),
  listRuntimesQueryKey: () => ["runtimes"],
  refreshRuntimeMutation: () => ({ mutationFn: refreshOne }),
  refreshRuntimesMutation: () => ({ mutationFn: refreshAll }),
  updateRuntimeEnvironmentMutation: () => ({ mutationFn: updateRuntime }),
}));

const runtimeObservers = vi.hoisted(() => new Set<GlobalEventObserver>());
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    runtimeObservers.add(observer);
    return () => runtimeObservers.delete(observer);
  },
}));
function RuntimeEvents() {
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [["runtimes"]],
  });
  return null;
}
function runtimeChanged() {
  act(() => {
    for (const observer of runtimeObservers)
      observer.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "runtimes",
        type: "runtimes.changed",
        resource: { kind: "global" },
        eventId: "event",
        payload: {},
      });
  });
}
function renderSection(queryClient: QueryClient, onOpenAccounts?: () => void) {
  return render(
    <QueryClientProvider client={queryClient}>
      <RuntimeEvents />
      <TooltipProvider delayDuration={0}>
        <RuntimeEnvironmentsSection onOpenAccounts={onOpenAccounts} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

async function waitForRuntimesLoaded() {
  await screen.findByRole("button", { name: "展开Codex智能体设置" });
  expect(
    screen.queryByRole("status", { name: "正在读取智能体" }),
  ).not.toBeInTheDocument();
}

beforeEach(() => {
  refreshAll.mockClear();
  refreshOne.mockClear();
  updateRuntime.mockClear();
  nativeModels.mockReset();
  thinking.mockReset();
  accounts.current = [];
  runtimes.current = [
    {
      id: "codex",
      name: "Codex CLI",
      enabled: true,
      isDefault: true,
      state: "ready",
      installed: true,
      version: "1.2.3",
      binary: "codex",
      prefixArgs: [],
      configHome: "",
      configHomeSupported: true,
      defaultModel: "gpt-5",
      defaultReasoning: "high",
      defaultPermissionLevel: "full-access",
      customModels: [],
      models: [
        {
          id: "gpt-5",
          name: "GPT-5",
          permissionLevels: ["default", "auto", "full-access"],
          reasoningOptions: [
            { id: "high", name: "High" },
            { id: "low", name: "Low" },
          ],
        },
        {
          id: "gpt-fast",
          name: "GPT Fast",
          permissionLevels: ["default", "auto", "full-access"],
          reasoningOptions: [{ id: "low", name: "Low" }],
        },
      ],
      reasoningOptions: [
        { id: "high", name: "High" },
        { id: "low", name: "Low" },
      ],
      diagnostics: [],
    },
    {
      id: "claude",
      name: "Claude Code",
      enabled: true,
      state: "unavailable",
      installed: false,
      binary: "claude",
      prefixArgs: [],
      configHome: "",
      configHomeSupported: true,
      defaultPermissionLevel: "full-access",
      customModels: [],
      models: [],
      reasoningOptions: [],
      diagnostics: [{ message: "未找到 claude" }],
    },
  ];
});

describe("RuntimeEnvironmentsSection", () => {
  it("切换已发现的版本时立即保存并重新探测", async () => {
    runtimes.current[0] = {
      ...runtimes.current[0],
      binary: "/opt/old/codex",
      resolvedBinary: "/opt/old/codex",
      installations: [
        { path: "/opt/old/codex", version: "1.2.3", available: true },
        { path: "/opt/new/codex", version: "2.0.0", available: true },
      ],
    };
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Codex智能体设置" }),
    );
    const versions = screen.getByRole("combobox", {
      name: "Codex版本",
    });
    versions.focus();
    await user.keyboard("{Enter}{ArrowDown}{Enter}");
    await waitFor(() =>
      expect(updateRuntime).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { runtimeId: "codex" },
          body: expect.objectContaining({ binary: "/opt/new/codex" }),
        }),
        expect.anything(),
      ),
    );
    expect(screen.queryByRole("textbox", { name: "Codex安装路径" })).toBeNull();
  });

  it("仅有一个安装版本时隐藏版本字段", async () => {
    runtimes.current[0] = {
      ...runtimes.current[0],
      binary: "/opt/homebrew/bin/codex",
      resolvedBinary: "/opt/homebrew/bin/codex",
      installations: [
        {
          path: "/opt/homebrew/bin/codex",
          version: "0.161.0",
          available: true,
        },
      ],
    };
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Codex智能体设置" }),
    );
    expect(screen.queryByRole("combobox", { name: "Codex版本" })).toBeNull();
    expect(screen.getByRole("combobox", { name: "账号" })).toBeVisible();
  });

  it("账号列表只展示当前智能体支持的账号", async () => {
    accounts.current = [
      {
        id: "chatgpt-subscription",
        name: "ChatGPT 订阅",
        authMode: "subscription",
        endpoints: [
          {
            protocol: "openai-responses",
            baseUrl: "https://api.openai.com/v1",
          },
        ],
      },
      {
        id: "deepseek",
        name: "DeepSeek",
        authMode: "api-key",
        endpoints: [{ protocol: "openai-responses" }],
      },
      {
        id: "volcengine",
        name: "火山方舟",
        authMode: "api-key",
        endpoints: [{ protocol: "anthropic-messages" }],
      },
    ];
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Claude智能体设置" }),
    );
    const account = screen.getByRole("combobox", { name: "账号" });
    account.focus();
    await user.keyboard("{Enter}");

    expect(screen.getByRole("option", { name: "内置账号" })).toBeVisible();
    expect(screen.getByRole("option", { name: "火山方舟" })).toBeVisible();
    expect(screen.queryByRole("option", { name: "ChatGPT 订阅" })).toBeNull();
    expect(screen.queryByRole("option", { name: "DeepSeek" })).toBeNull();
  });

  it("切换账号后只显示账号模型和当前模型的原生推理选项", async () => {
    accounts.current = [
      {
        id: "deepseek-account",
        name: "DeepSeek",
        authMode: "api-key",
        revision: 1,
        endpoints: [
          { protocol: "openai-responses", baseUrl: "https://api.deepseek.com" },
        ],
        selectedModels: ["deepseek-flash"],
        defaultModel: "deepseek-flash",
        defaultReasoning: "",
      },
    ];
    thinking.mockResolvedValue({
      data: { available: true, options: [{ id: "max", name: "Max" }] },
    });
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Codex智能体设置" }),
    );
    const account = screen.getByRole("combobox", { name: "账号" });
    account.focus();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(account).toHaveTextContent("DeepSeek");

    const model = screen.getByRole("combobox", { name: "默认模型" });
    expect(model).toHaveTextContent("deepseek-flash");
    model.focus();
    await user.keyboard("{Enter}");
    expect(
      screen.getByRole("option", { name: "deepseek-flash" }),
    ).toBeVisible();
    expect(screen.queryByRole("option", { name: "GPT-5" })).toBeNull();
    await user.keyboard("{Escape}");

    await waitFor(() =>
      expect(thinking).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { accountId: "deepseek-account" },
          query: { runtimeId: "codex", model: "deepseek-flash" },
        }),
      ),
    );
    const reasoning = screen.getByRole("combobox", {
      name: "默认推理强度",
    });
    expect(reasoning).toHaveTextContent("默认");
    reasoning.focus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("option", { name: "Max" })).toBeVisible();
    expect(screen.queryByRole("option", { name: "High" })).toBeNull();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(reasoning).toHaveTextContent("Max");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(updateRuntime).toHaveBeenCalledWith(
        expect.objectContaining({
          body: expect.objectContaining({
            defaultAccountId: "deepseek-account",
            defaultModel: "deepseek-flash",
            defaultReasoning: "max",
            customModels: [],
          }),
        }),
        expect.anything(),
      ),
    );
  });

  it("订阅账号加载原生模型，切回系统配置后恢复智能体选项", async () => {
    accounts.current = [
      {
        id: "subscription-account",
        name: "ChatGPT",
        authMode: "subscription",
        revision: 2,
        endpoints: [
          {
            protocol: "openai-responses",
            baseUrl: "https://api.openai.com/v1",
          },
        ],
        defaultModel: "",
        defaultReasoning: "",
      },
    ];
    nativeModels.mockResolvedValue({
      data: { models: ["gpt-native"], defaultModel: "gpt-native" },
    });
    thinking.mockResolvedValue({
      data: { available: false, options: [] },
    });
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Codex智能体设置" }),
    );
    const account = screen.getByRole("combobox", { name: "账号" });
    account.focus();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    const model = screen.getByRole("combobox", { name: "默认模型" });
    await waitFor(() => expect(model).toHaveTextContent("gpt-native"));
    expect(nativeModels).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { accountId: "subscription-account" },
        query: { runtimeId: "codex" },
      }),
    );
    expect(
      screen.getByRole("combobox", { name: "默认推理强度" }),
    ).toHaveTextContent("默认");

    account.focus();
    await user.keyboard("{Enter}{Home}{Enter}");
    expect(account).toHaveTextContent("内置账号");
    expect(model).toHaveTextContent("GPT-5");
    expect(
      screen.getByRole("combobox", { name: "默认推理强度" }),
    ).toHaveTextContent("High");
  });

  it("展开区域与行内操作相互独立，使用键盘也能展开和收起", async () => {
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    const trigger = await screen.findByRole("button", {
      name: "展开Codex智能体设置",
    });
    expect(
      screen.queryByRole("button", { name: /Codex：.*查看处理建议/ }),
    ).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.getAllByText("默认")).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "重新探测Codex" }));
    await waitFor(() => expect(refreshOne).toHaveBeenCalledOnce());
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    trigger.focus();
    await user.keyboard("{Enter}");
    expect(
      screen.getByRole("button", { name: "收起Codex智能体设置" }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("combobox", { name: "账号" })).toBeVisible();
    await user.click(screen.getByRole("switch", { name: "Codex启用状态" }));
    await waitFor(() => expect(updateRuntime).toHaveBeenCalledOnce());
    expect(
      screen.getByRole("button", { name: "收起Codex智能体设置" }),
    ).toHaveAttribute("aria-expanded", "true");
    await user.click(
      screen.getByRole("button", { name: "收起Codex智能体设置" }),
    );
    expect(
      screen.queryByRole("combobox", { name: "账号" }),
    ).not.toBeInTheDocument();
  });

  it("分组展示未发现的受支持运行时，并保留命令配置入口", async () => {
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await waitForRuntimesLoaded();
    expect(screen.getByRole("heading", { name: "已发现" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "未发现" })).toBeInTheDocument();
    expect(
      screen.getByText("Claude", { selector: "span" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("未找到 claude")).not.toBeInTheDocument();
    expect(screen.getByText("未安装")).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Claude启用状态" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("switch", { name: "Claude启用状态" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "展开Claude智能体设置" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "重新探测Claude" }),
    ).toBeEnabled();
    await user.click(
      screen.getByRole("button", { name: "展开Claude智能体设置" }),
    );
    expect(screen.getByRole("combobox", { name: "账号" })).toBeVisible();
    expect(updateRuntime).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() => expect(refreshAll).toHaveBeenCalledOnce());
  });

  it("已安装但未登录或协议不可用的智能体仍列在已发现分组", async () => {
    const claude = runtimes.current[1];
    if (!claude) throw new Error("缺少 Claude 测试数据");
    runtimes.current = [
      {
        ...runtimes.current[0],
        id: "kimi",
        name: "Kimi",
        auth: "missing",
        version: "",
      },
      {
        ...claude,
        id: "cursor",
        name: "Cursor",
        installed: true,
        state: "incompatible",
      },
      claude,
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await screen.findByRole("button", { name: "展开Kimi智能体设置" });
    const found = screen
      .getByRole("heading", { name: "已发现" })
      .closest("section");
    const missing = screen
      .getByRole("heading", { name: "未发现" })
      .closest("section");
    if (!found || !missing) throw new Error("缺少智能体分组");
    expect(
      within(found).getByRole("button", { name: "展开Kimi智能体设置" }),
    ).toBeInTheDocument();
    expect(within(found).getByText("CLI 未登录")).toBeInTheDocument();
    expect(
      within(found).getByRole("button", { name: "展开Cursor智能体设置" }),
    ).toBeInTheDocument();
    expect(within(found).getByText("协议不兼容")).toBeInTheDocument();
    expect(
      within(found).getByRole("button", {
        name: "Cursor：协议不兼容，查看处理建议",
      }),
    ).toBeInTheDocument();
    expect(
      within(missing).getByRole("button", { name: "展开Claude智能体设置" }),
    ).toBeInTheDocument();
    expect(within(missing).getByText("未安装")).toBeInTheDocument();
    expect(
      within(found).getByRole("switch", { name: "Kimi启用状态" }),
    ).toBeEnabled();
    expect(
      within(found).getByRole("switch", { name: "Cursor启用状态" }),
    ).toBeDisabled();
  });

  it("未登录的 CLI 在已发现行显示告警图标和自行登录建议", async () => {
    const user = userEvent.setup();
    runtimes.current = [
      {
        ...runtimes.current[0],
        id: "kimi",
        name: "Kimi",
        auth: "missing",
        version: "2.0.2",
        diagnostics: [{ code: "ACP_OPTIONS_UNAVAILABLE", level: "error" }],
      },
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );

    const alert = await screen.findByRole("button", {
      name: "Kimi：CLI 未登录，查看处理建议",
    });
    expect(alert.closest("section")).toHaveTextContent("已发现");
    expect(
      screen.getByRole("button", { name: "展开Kimi智能体设置" }),
    ).toHaveTextContent("2.0.2");
    await user.click(alert);
    expect(
      screen.getByRole("dialog", { name: "CLI 未登录" }),
    ).toHaveTextContent("在 Kimi CLI 中完成登录或配置可用的提供商");
    expect(
      within(screen.getByRole("dialog")).getAllByRole("button", {
        name: "关闭",
      }),
    ).toHaveLength(1);
    expect(
      within(screen.getByRole("dialog")).queryByText(/^close$/i),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "前往 AI 账号" }),
    ).not.toBeInTheDocument();
  });

  it("关联账号缺少凭据时显示账号告警并可前往账号设置", async () => {
    const user = userEvent.setup();
    const onOpenAccounts = vi.fn();
    accounts.current = [
      {
        id: "account-1",
        name: "工作账号",
        authMode: "api-key",
        hasCredential: false,
      },
    ];
    runtimes.current = [
      {
        ...runtimes.current[0],
        defaultAccountId: "account-1",
        auth: "unknown",
        diagnostics: [],
      },
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
      onOpenAccounts,
    );

    const alert = await screen.findByRole("button", {
      name: "Codex：关联账号缺少密钥，查看处理建议",
    });
    await user.click(alert);
    expect(
      screen.getByRole("dialog", { name: "关联账号缺少密钥" }),
    ).toHaveTextContent("工作账号");
    await user.click(screen.getByRole("button", { name: "前往 AI 账号" }));
    expect(onOpenAccounts).toHaveBeenCalledOnce();
  });

  it("关联账号准备失败时优先提示账号问题", async () => {
    accounts.current = [
      {
        id: "account-1",
        name: "工作账号",
        authMode: "api-key",
        hasCredential: true,
      },
    ];
    runtimes.current = [
      {
        ...runtimes.current[0],
        defaultAccountId: "account-1",
        auth: "missing",
        diagnostics: [{ code: "AI_ACCOUNT_UNAVAILABLE", level: "warning" }],
      },
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );

    expect(
      await screen.findByRole("button", {
        name: "Codex：关联账号不可用，查看处理建议",
      }),
    ).toBeInTheDocument();
  });

  it("关联账号的模型目录探测失败时提示检查服务地址和网络", async () => {
    const user = userEvent.setup();
    accounts.current = [
      {
        id: "account-1",
        name: "工作账号",
        authMode: "api-key",
        hasCredential: true,
      },
    ];
    runtimes.current = [
      {
        ...runtimes.current[0],
        defaultAccountId: "account-1",
        auth: "unknown",
        diagnostics: [{ code: "ACP_OPTIONS_UNAVAILABLE", level: "warning" }],
      },
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );

    await user.click(
      await screen.findByRole("button", {
        name: "Codex：模型目录不可用，查看处理建议",
      }),
    );
    expect(
      screen.getByRole("dialog", { name: "模型目录不可用" }),
    ).toHaveTextContent("服务地址和网络");
  });

  it("代理标题不展示底层 ACP 命令", async () => {
    runtimes.current = [
      { ...runtimes.current[0], id: "pi", name: "Pi", binary: "pi-acp" },
    ];
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await screen.findByText("Pi");
    expect(screen.queryByText("pi-acp")).not.toBeInTheDocument();
  });

  it("切换环境默认模型时只展示该 ACP 模型支持的 Thinking", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderSection(queryClient);

    await user.click(
      await screen.findByRole("button", {
        name: "展开Codex智能体设置",
      }),
    );
    const model = screen.getByRole("combobox", { name: "默认模型" });
    model.focus();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    const reasoning = screen.getByRole("combobox", {
      name: "默认推理强度",
    });
    expect(reasoning).toHaveTextContent("Low");
    reasoning.focus();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(reasoning).toHaveTextContent("Low");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updateRuntime).toHaveBeenCalledTimes(1));
    expect(updateRuntime).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          defaultModel: "gpt-fast",
          defaultReasoning: "low",
        }),
      }),
      expect.anything(),
    );
  });

  it("按实际权限推导 YOLO，并在切回手动后解锁单项权限", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderSection(queryClient);

    await waitForRuntimesLoaded();
    expect(screen.getByRole("heading", { name: "未发现" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "YOLO" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(
      screen.getByRole("button", {
        name: "Codex默认权限：完全访问",
      }),
    ).toBeDisabled();
    await user.click(screen.getByRole("radio", { name: "手动" }));
    expect(
      screen.getByRole("button", {
        name: "Codex默认权限：完全访问",
      }),
    ).toBeEnabled();

    await user.click(
      screen.getByRole("button", { name: "展开Codex智能体设置" }),
    );
    expect(screen.getByRole("combobox", { name: "默认模型" })).toHaveClass(
      "h-control-md",
    );
    const reasoning = screen.getByRole("combobox", {
      name: "默认推理强度",
    });
    reasoning.focus();
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updateRuntime).toHaveBeenCalledTimes(1));
    expect(updateRuntime).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          defaultReasoning: "low",
        }),
        path: { runtimeId: "codex" },
      }),
      expect.anything(),
    );
  });

  it("手动模式使用与聊天一致的权限选择器修改 Runtime 默认权限", async () => {
    runtimes.current[0] = {
      ...runtimes.current[0],
      defaultPermissionLevel: "auto",
    };
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderSection(queryClient);

    await screen.findByRole("button", {
      name: "展开Codex智能体设置",
    });
    expect(screen.getByRole("radio", { name: "手动" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    const permission = screen.getByRole("button", {
      name: "Codex默认权限：自动审批",
    });
    await user.click(permission);
    await user.click(screen.getByText("默认权限"));

    await waitFor(() => expect(updateRuntime).toHaveBeenCalledOnce());
    expect(updateRuntime).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({ defaultPermissionLevel: "default" }),
        path: { runtimeId: "codex" },
      }),
      expect.anything(),
    );
  });

  it("确认切换 YOLO 后把全部已发现 CLI 更新为完全访问", async () => {
    runtimes.current = runtimes.current.map((runtime, index) => ({
      ...runtime,
      state: "ready",
      installed: true,
      defaultModel: "gpt-5",
      defaultPermissionLevel: index === 0 ? "auto" : "default",
      models:
        Array.isArray(runtime.models) && runtime.models.length > 0
          ? runtime.models
          : [
              {
                id: "gpt-5",
                name: "GPT-5",
                permissionLevels: ["default", "auto", "full-access"],
              },
            ],
    }));
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderSection(queryClient);

    await user.click(await screen.findByRole("radio", { name: "YOLO" }));
    const dialog = screen.getByRole("dialog", { name: "启用 YOLO 模式？" });
    expect(dialog).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "关闭" }).closest("header"),
    ).not.toContainElement(
      screen.getByText(/这会把全部已发现智能体的默认权限改为完全访问/),
    );
    expect(screen.getByRole("button", { name: "取消" })).toHaveClass("border");
    expect(updateRuntime).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "确认启用 YOLO" }));

    await waitFor(() => expect(updateRuntime).toHaveBeenCalledTimes(2));
    expect(updateRuntime).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        path: { runtimeId: "codex" },
        body: expect.objectContaining({
          defaultPermissionLevel: "full-access",
        }),
      }),
      expect.anything(),
    );
    expect(updateRuntime).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        path: { runtimeId: "claude" },
        body: expect.objectContaining({
          defaultPermissionLevel: "full-access",
        }),
      }),
      expect.anything(),
    );
  });

  it("模型不支持完全访问时禁用 YOLO 且不写入该档", async () => {
    runtimes.current = [
      {
        ...runtimes.current[0],
        defaultPermissionLevel: "auto",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto"],
          },
        ],
      },
      {
        ...runtimes.current[1],
        state: "ready",
        defaultModel: "sonnet",
        defaultPermissionLevel: "default",
        models: [
          {
            id: "sonnet",
            name: "Sonnet",
            permissionLevels: ["default", "auto"],
          },
        ],
      },
    ];
    renderSection(
      new QueryClient({
        defaultOptions: { queries: { retry: false } },
      }),
    );

    await waitForRuntimesLoaded();
    expect(screen.getByRole("radio", { name: "YOLO" })).toBeDisabled();
    expect(screen.getByRole("radio", { name: "YOLO" })).toHaveClass(
      "opacity-45",
    );
    expect(screen.getByRole("radio", { name: "手动" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(
      screen.getByText("部分已发现智能体不支持完全访问，无法启用 YOLO。"),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText(
        "当前模型不支持完全访问，默认权限已按可表达的最高等级保存。",
      ).length,
    ).toBeGreaterThan(0);
    expect(updateRuntime).not.toHaveBeenCalled();
  });

  it("探测未声明权限时不发明三档", async () => {
    runtimes.current[0] = {
      ...runtimes.current[0],
      defaultPermissionLevel: "full-access",
      models: [{ id: "default", name: "Default" }],
    };
    const user = userEvent.setup();
    renderSection(
      new QueryClient({
        defaultOptions: { queries: { retry: false } },
      }),
    );

    await waitForRuntimesLoaded();
    expect(screen.getByRole("radio", { name: "YOLO" })).toBeDisabled();
    const permission = screen.getByRole("button", {
      name: "Codex默认权限：完全访问",
    });
    expect(permission).toBeDisabled();
    await user.click(screen.getByRole("radio", { name: "手动" }));
    expect(permission).toBeDisabled();
    expect(updateRuntime).not.toHaveBeenCalled();
  });

  it("允许选择目录中的模型并将它保存为默认模型", async () => {
    const user = userEvent.setup();
    renderSection(
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    );
    await user.click(
      await screen.findByRole("button", { name: "展开Codex智能体设置" }),
    );
    const model = screen.getByRole("combobox", { name: "默认模型" });
    model.focus();
    await user.keyboard("{Enter}");
    await user.click(screen.getByRole("option", { name: "GPT Fast" }));
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(updateRuntime).toHaveBeenCalledWith(
        expect.objectContaining({
          body: expect.objectContaining({ defaultModel: "gpt-fast" }),
        }),
        expect.anything(),
      ),
    );
  });
});

it.each([
  ["kimi", "Kimi", "kimi"],
  ["cursor", "Cursor", "cursor-agent"],
  ["pi", "Pi", "pi"],
])("保存 %s 的 CLI 路径与动态模型选择", async (id, name, command) => {
  runtimes.current = [
    {
      ...runtimes.current[0],
      id,
      name,
      binary: command,
      models: [
        { id: "default", name: "Default", permissionLevels: ["full-access"] },
        {
          id: "provider/model-b",
          name: "Model B",
          permissionLevels: ["full-access"],
        },
      ],
      configHomeSupported: false,
    },
  ];
  const user = userEvent.setup();
  renderSection(
    new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  await user.click(
    await screen.findByRole("button", { name: `展开${name}智能体设置` }),
  );
  const model = screen.getByRole("combobox", { name: "默认模型" });
  model.focus();
  await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
  expect(model).toHaveTextContent("Model B");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(updateRuntime).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: id },
        body: expect.objectContaining({
          defaultModel: "provider/model-b",
        }),
      }),
      expect.anything(),
    ),
  );
});

it("首次探测条目展示正在探测，快照更新后显示版本", async () => {
  runtimes.current = [
    {
      ...runtimes.current[0],
      detectedAt: 0,
      state: "",
      installed: false,
      version: "",
    },
  ];
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  renderSection(client);
  await screen.findByText("正在探测");
  expect(screen.queryByText("未安装")).not.toBeInTheDocument();
  runtimes.current = [
    {
      ...runtimes.current[0],
      detectedAt: 1,
      state: "ready",
      installed: true,
      version: "2.0.0",
    },
  ];
  runtimeChanged();
  await screen.findByText("2.0.0", {}, { timeout: 2500 });
  expect(screen.queryByText("正在探测")).not.toBeInTheDocument();
});

it("已安装智能体的 unknown 版本展示为版本未知", async () => {
  runtimes.current = [{ ...runtimes.current[0], version: "unknown" }];
  renderSection(
    new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  await screen.findByText("版本未知");
  expect(screen.queryByText("unknown")).not.toBeInTheDocument();
});
