import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { AIAccountsSection } from "./ai-accounts-section";
import "@/shared/i18n";

const { create, update, preview, refresh, accounts, list, readCredential } =
  vi.hoisted(() => ({
    list: vi.fn(),
    create: vi.fn(async () => ({ id: "new-account" })),
    update: vi.fn(async () => ({})),
    preview: vi.fn(async () => ({
      resources: [] as Array<{ id: string; name: string }>,
    })),
    refresh: vi.fn(async () => ({
      resources: [] as Array<{ id: string; name: string }>,
    })),
    accounts: { current: [] as Array<Record<string, unknown>> },
    readCredential: vi.fn(async () => ({ data: { apiKey: "saved-secret" } })),
  }));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getAiAccountCredential: readCredential,
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({ queryKey: ["accounts"], queryFn: list }),
  listAiAccountsQueryKey: () => ["accounts"],
  createAiAccountMutation: () => ({ mutationFn: create }),
  updateAiAccountMutation: () => ({ mutationFn: update }),
  previewAiAccountModelsMutation: () => ({ mutationFn: preview }),
  refreshAiAccountModelsMutation: () => ({ mutationFn: refresh }),
  deleteAiAccountMutation: () => ({ mutationFn: vi.fn() }),
}));
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.scrollIntoView = () => undefined;
});
afterAll(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
  Reflect.deleteProperty(Element.prototype, "setPointerCapture");
  Reflect.deleteProperty(Element.prototype, "releasePointerCapture");
  Reflect.deleteProperty(Element.prototype, "scrollIntoView");
});
beforeEach(() => {
  create.mockClear();
  update.mockClear();
  preview.mockReset().mockResolvedValue({ resources: [] });
  refresh.mockReset().mockResolvedValue({ resources: [] });
  readCredential
    .mockReset()
    .mockResolvedValue({ data: { apiKey: "saved-secret" } });
  accounts.current = [];
  list
    .mockReset()
    .mockImplementation(async () => ({ resources: accounts.current }));
});

it("编辑已有账号回显 Key，默认隐藏且未修改时不重复替换", async () => {
  accounts.current = [
    {
      id: "saved-key",
      name: "密钥账号",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://api.example.com/v1" },
      ],
      selectedModels: ["model-a"],
      modelCatalog: ["model-a"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 密钥账号" }),
  );
  const key = await screen.findByLabelText("API Key");
  await waitFor(() => expect(key).toHaveValue("saved-secret"));
  expect(key).toHaveAttribute("type", "password");
  await user.click(screen.getByRole("button", { name: "显示 API Key" }));
  expect(key).toHaveAttribute("type", "text");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({ apiKey: "" }),
      }),
      expect.anything(),
    ),
  );
});
function renderAccounts() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <AIAccountsSection />
    </QueryClientProvider>,
  );
}

it("每个账号只保存一个请求协议，API 地址不再选择格式", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  const key = screen.getByLabelText("API Key");
  expect(screen.getByRole("combobox", { name: "请求协议" })).toBeVisible();
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.openai.com/v1",
  );
  expect(
    screen.queryByRole("button", { name: "添加协议" }),
  ).not.toBeInTheDocument();
  await user.type(key, "test-key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.openai.com/v1",
            },
          ],
        }),
      }),
      expect.anything(),
    ),
  );
});

it("内置服务商切换协议时同步地址，账号只保存当前协议", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "DeepSeek" }));
  expect(screen.getByRole("textbox", { name: "账号名称" })).toHaveValue(
    "DeepSeek",
  );
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.deepseek.com",
  );
  await user.click(screen.getByRole("combobox", { name: "请求协议" }));
  await user.click(screen.getByRole("option", { name: "Anthropic Messages" }));
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.deepseek.com/anthropic",
  );
  await user.type(screen.getByLabelText("API Key"), "deepseek-key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          endpoints: [
            {
              protocol: "anthropic-messages",
              baseUrl: "https://api.deepseek.com/anthropic",
            },
          ],
        }),
      }),
      expect.anything(),
    ),
  );
});

it("Kimi 统一展示 API 与套餐选项，套餐仍保存为 Key 账号", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "Kimi" }));
  expect(screen.getByRole("textbox", { name: "账号名称" })).toHaveValue("Kimi");
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.moonshot.cn/v1",
  );
  await user.click(screen.getByRole("combobox", { name: "认证方式" }));
  expect(screen.queryByRole("option", { name: "ChatGPT 订阅" })).toBeNull();
  await user.click(screen.getByRole("option", { name: "订阅套餐（API Key）" }));
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.kimi.com/coding/v1",
  );
  await user.click(screen.getByRole("combobox", { name: "请求协议" }));
  await user.click(screen.getByRole("option", { name: "Anthropic Messages" }));
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://api.kimi.com/coding",
  );
  await user.type(screen.getByLabelText("API Key"), "kimi-plan-key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          authMode: "api-key",
          endpoints: [
            {
              protocol: "anthropic-messages",
              baseUrl: "https://api.kimi.com/coding",
            },
          ],
        }),
      }),
      expect.anything(),
    ),
  );
});

it("火山方舟按套餐切换地址，通义千问只显示短名称", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "火山方舟" }));
  await user.click(screen.getByRole("combobox", { name: "认证方式" }));
  expect(screen.queryByRole("option", { name: "ChatGPT 订阅" })).toBeNull();
  await user.click(screen.getByRole("option", { name: "订阅套餐（API Key）" }));
  expect(screen.getByRole("combobox", { name: "套餐类型" })).toBeVisible();
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://ark.cn-beijing.volces.com/api/coding/v3",
  );
  await user.click(screen.getByRole("combobox", { name: "套餐类型" }));
  await user.click(screen.getByRole("option", { name: "Agent Plan" }));
  expect(screen.getByRole("textbox", { name: "API 地址" })).toHaveValue(
    "https://ark.cn-beijing.volces.com/api/plan/v3",
  );
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "通义千问" }));
  expect(screen.getByRole("textbox", { name: "账号名称" })).toHaveValue(
    "通义千问",
  );
  expect(screen.queryByRole("combobox", { name: "认证方式" })).toBeNull();
  expect(screen.getByText("API Key", { selector: "p" })).toBeVisible();
});

it("ChatGPT 订阅选项只出现在 OpenAI，自定义仅使用 API Key", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("combobox", { name: "认证方式" }));
  expect(screen.getByRole("option", { name: "ChatGPT 订阅" })).toBeVisible();
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "自定义" }));
  expect(screen.queryByRole("combobox", { name: "认证方式" })).toBeNull();
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "OpenAI" }));
  expect(screen.getByRole("combobox", { name: "认证方式" })).toBeVisible();
});

it("已保存的 ChatGPT 订阅账号不能改选其他服务商", async () => {
  accounts.current = [
    {
      id: "saved-subscription",
      name: "我的订阅",
      authMode: "subscription",
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://api.openai.com/v1" },
      ],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 我的订阅" }),
  );
  expect(screen.getByText("ChatGPT 订阅", { selector: "p" })).toBeVisible();
  expect(screen.queryByRole("combobox", { name: "服务商" })).toBeNull();
  expect(screen.queryByRole("combobox", { name: "认证方式" })).toBeNull();
});

it("编辑 Kimi 账号切换套餐时要求输入对应套餐的 Key", async () => {
  accounts.current = [
    {
      id: "saved-kimi",
      name: "Kimi 工作账号",
      authMode: "api-key",
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://api.moonshot.cn/v1" },
      ],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 Kimi 工作账号" }),
  );
  const key = screen.getByLabelText("API Key");
  expect(key).not.toBeRequired();
  await user.click(screen.getByRole("combobox", { name: "认证方式" }));
  await user.click(screen.getByRole("option", { name: "订阅套餐（API Key）" }));
  expect(key).toBeRequired();
  expect(key).toHaveAttribute("placeholder", "输入 API Key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(update).not.toHaveBeenCalled();
  await user.type(key, "plan-key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({ apiKey: "plan-key" }),
      }),
      expect.anything(),
    ),
  );
});

it("服务商选择框旁的官网按钮打开对应网站", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("button", { name: "访问OpenAI官网" }));
  expect(open).toHaveBeenCalledWith(
    "https://platform.openai.com/",
    "_blank",
    "noreferrer",
  );
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(screen.getByRole("option", { name: "自定义" }));
  expect(screen.queryByRole("button", { name: /访问.*官网/ })).toBeNull();
  open.mockRestore();
});

it("刷新只更新候选模型，搜索后手动选择并在再次刷新时保留", async () => {
  const user = userEvent.setup();
  preview.mockResolvedValueOnce({
    resources: [
      { id: "model-a", name: "model-a" },
      { id: "model-b", name: "model-b" },
    ],
  });
  preview.mockResolvedValueOnce({
    resources: [
      { id: "model-a", name: "model-a" },
      { id: "model-c", name: "model-c" },
    ],
  });
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  await user.type(screen.getByLabelText("API Key"), "test-key");
  await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
  expect(
    await screen.findByRole("button", {
      name: "共 2 个模型，已选择 0 个",
    }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "移除自定义模型 model-b" }),
  ).toBeNull();
  await user.click(
    screen.getByRole("button", { name: "共 2 个模型，已选择 0 个" }),
  );
  await user.type(
    screen.getByRole("searchbox", { name: "搜索模型" }),
    "model-a",
  );
  expect(screen.getByRole("checkbox", { name: "model-a" })).toBeVisible();
  expect(screen.queryByRole("checkbox", { name: "model-b" })).toBeNull();
  await user.click(screen.getByRole("checkbox", { name: "model-a" }));
  await user.click(screen.getByRole("button", { name: "清空已选" }));
  expect(
    screen.getByRole("button", { name: "共 2 个模型，已选择 0 个" }),
  ).toBeVisible();
  await user.click(screen.getByRole("checkbox", { name: "model-a" }));
  await user.click(
    screen.getByRole("button", { name: "共 2 个模型，已选择 1 个" }),
  );
  await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
  await waitFor(() => expect(preview).toHaveBeenCalledTimes(2));
  expect(
    screen.getByRole("button", { name: "共 2 个模型，已选择 1 个" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelCatalog: ["model-a", "model-c"],
          selectedModels: ["model-a"],
          defaultModel: "model-a",
        }),
      }),
      expect.anything(),
    ),
  );
});

it("上下文长度直接跟随已选模型，并保存用户修改", async () => {
  accounts.current = [
    {
      id: "saved-context",
      name: "模型账号",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        { protocol: "openai-completions", baseUrl: "https://example.com/v1" },
      ],
      selectedModels: ["model-a"],
      modelCatalog: ["model-a"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 模型账号" }),
  );
  const contextSize = screen.getByRole("spinbutton", {
    name: "model-a 的上下文长度（tokens）",
  });
  expect(contextSize).toHaveValue(262144);
  expect(screen.queryByText(/高级 · Kimi Code/)).not.toBeInTheDocument();
  await user.clear(contextSize);
  await user.type(contextSize, "131072");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelContextSizes: { "model-a": 131072 },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("模型映射随账号保存，Claude 角色只引用已选模型", async () => {
  accounts.current = [
    {
      id: "mapped-account",
      name: "映射账号",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        { protocol: "anthropic-messages", baseUrl: "https://example.com" },
      ],
      selectedModels: ["fast", "strong"],
      modelCatalog: ["fast", "strong"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 映射账号" }),
  );
  await user.click(screen.getByRole("combobox", { name: "Opus 使用的模型" }));
  await user.click(screen.getByRole("option", { name: "strong" }));
  await user.type(
    screen.getByRole("textbox", { name: "Opus 的菜单显示名" }),
    "强力模型",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelDisplayNames: { strong: "强力模型" },
          claudeModelRoles: { opus: "strong" },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("DeepSeek Responses 使用 服务商目录默认值并将映射留在高级中", async () => {
  accounts.current = [
    {
      id: "deepseek-responses",
      name: "DeepSeek Responses",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://api.deepseek.com" },
      ],
      selectedModels: ["deepseek-flash"],
      modelCatalog: ["deepseek-flash"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 DeepSeek Responses" }),
  );
  expect(screen.getByRole("button", { name: "高级" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  expect(
    screen.getByRole("textbox", { name: "deepseek-flash 的菜单显示名" }),
  ).toHaveValue("DeepSeek V4.1 Flash");
  expect(
    screen.getByRole("spinbutton", {
      name: "deepseek-flash 的上下文长度（tokens）",
    }),
  ).toHaveValue(1048576);
  expect(screen.queryByText(/思考等级用于/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelDisplayNames: { "deepseek-flash": "DeepSeek V4.1 Flash" },
          modelContextSizes: { "deepseek-flash": 1048576 },
          modelReasoningLevels: {
            "deepseek-flash": ["low", "high", "max"],
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("没有预设映射的 Responses 模型默认选择 Low、Medium、High", async () => {
  accounts.current = [
    {
      id: "doubao-legacy",
      name: "火山方舟旧模型",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        {
          protocol: "openai-responses",
          baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
        },
      ],
      selectedModels: ["doubao-seed-1-6-flash-250615"],
      modelCatalog: ["doubao-seed-1-6-flash-250615"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 火山方舟旧模型" }),
  );
  expect(
    screen.getByRole("button", {
      name: "doubao-seed-1-6-flash-250615 的思考等级",
    }),
  ).toHaveTextContent("low, medium, high");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelReasoningLevels: {
            "doubao-seed-1-6-flash-250615": ["low", "medium", "high"],
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("火山方舟当前 Codex 预设带出 服务商模型元数据", async () => {
  accounts.current = [
    {
      id: "doubao-current",
      name: "火山方舟新模型",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        {
          protocol: "openai-responses",
          baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
        },
      ],
      selectedModels: ["doubao-seed-2-1-pro-260628"],
      modelCatalog: ["doubao-seed-2-1-pro-260628"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 火山方舟新模型" }),
  );
  expect(
    screen.getByRole("textbox", {
      name: "doubao-seed-2-1-pro-260628 的菜单显示名",
    }),
  ).toHaveValue("Doubao Seed 2.1 Pro");
  expect(
    screen.getByRole("spinbutton", {
      name: "doubao-seed-2-1-pro-260628 的上下文长度（tokens）",
    }),
  ).toHaveValue(262144);
  expect(
    screen.getByRole("button", {
      name: "doubao-seed-2-1-pro-260628 的思考等级",
    }),
  ).toHaveTextContent("minimal, low, medium, high");
});

it("DeepSeek Anthropic 只呈现 Claude 角色映射并套用已选模型的默认值", async () => {
  accounts.current = [
    {
      id: "deepseek-anthropic",
      name: "DeepSeek Anthropic",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        {
          protocol: "anthropic-messages",
          baseUrl: "https://api.deepseek.com/anthropic",
        },
      ],
      selectedModels: ["deepseek-v4-pro", "deepseek-v4-flash"],
      modelCatalog: ["deepseek-v4-pro", "deepseek-v4-flash"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 DeepSeek Anthropic" }),
  );
  expect(
    screen.getByRole("combobox", { name: "Sonnet 使用的模型" }),
  ).toHaveTextContent("deepseek-v4-pro");
  expect(
    screen.getByRole("combobox", { name: "Haiku 使用的模型" }),
  ).toHaveTextContent("deepseek-v4-flash");
  expect(screen.getAllByText("模型映射")).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          claudeModelRoles: {
            sonnet: "deepseek-v4-pro",
            opus: "deepseek-v4-pro",
            fable: "deepseek-v4-pro",
            haiku: "deepseek-v4-flash",
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("编辑账号回显已保存的 Key，未修改时仍保留原凭据", async () => {
  accounts.current = [
    {
      id: "saved",
      name: "账号 A",
      authMode: "api-key",
      revision: 2,
      hasCredential: true,
      endpoints: [
        {
          protocol: "anthropic-messages",
          baseUrl: "https://api.anthropic.com",
        },
      ],
      selectedModels: ["claude-model"],
      modelCatalog: ["claude-model"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 账号 A" }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("API Key")).toHaveValue("saved-secret"),
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { accountId: "saved" },
        body: expect.objectContaining({
          apiKey: "",
          revision: 2,
          selectedModels: ["claude-model"],
        }),
      }),
      expect.anything(),
    ),
  );
});

it("编辑账号修改地址后按当前表单刷新并复用已保存的 Key", async () => {
  accounts.current = [
    {
      id: "saved",
      name: "账号 A",
      authMode: "api-key",
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://old.example.com/v1" },
      ],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 账号 A" }),
  );
  const address = screen.getByRole("textbox", { name: "API 地址" });
  await user.clear(address);
  await user.type(address, "https://new.example.com/v1");
  await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
  await waitFor(() =>
    expect(preview).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://new.example.com/v1",
          },
          apiKey: "",
          accountId: "saved",
        },
      }),
      expect.anything(),
    ),
  );
  expect(refresh).not.toHaveBeenCalled();
});

it("账号详情按需展开，编辑操作不改变展开状态", async () => {
  accounts.current = [
    {
      id: "saved",
      name: "工作账号",
      authMode: "api-key",
      hasCredential: true,
      endpoints: [
        {
          protocol: "anthropic-messages",
          baseUrl: "https://api.example.com/anthropic",
        },
      ],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  const expand = await screen.findByRole("button", {
    name: "展开账号 工作账号",
  });
  expect(expand).toHaveAttribute("aria-expanded", "false");
  expect(
    screen.queryByText("https://api.example.com/anthropic"),
  ).not.toBeInTheDocument();
  await user.click(expand);
  expect(screen.getByText("https://api.example.com/anthropic")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "编辑账号 工作账号" }));
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(
    screen.getByRole("button", { name: "收起账号 工作账号" }),
  ).toHaveAttribute("aria-expanded", "true");
});

it("新增账号去除首尾空白后不能与现有账号重名", async () => {
  accounts.current = [
    {
      id: "saved",
      name: "OpenAI",
      authMode: "api-key",
      hasCredential: true,
      endpoints: [],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await screen.findByRole("button", { name: "编辑账号 OpenAI" });
  await user.click(screen.getByRole("button", { name: "添加账号" }));
  const name = screen.getByRole("textbox", { name: "账号名称" });
  expect(name).toHaveAttribute("aria-invalid", "true");
  expect(screen.getByRole("alert")).toHaveTextContent("账号名称已存在");
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  await user.clear(name);
  await user.type(name, " OpenAI ");
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  await user.clear(name);
  await user.type(name, "OpenAI 工作");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
  expect(create).not.toHaveBeenCalled();
});

it("编辑保留自身名称可保存，但不能改为其他账号名称", async () => {
  accounts.current = ["账号 A", "账号 B"].map((name, index) => ({
    id: `saved-${index}`,
    name,
    authMode: "api-key",
    hasCredential: true,
    endpoints: [
      { protocol: "openai-responses", baseUrl: "https://api.openai.com/v1" },
    ],
  }));
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 账号 A" }),
  );
  expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
  const name = screen.getByRole("textbox", { name: "账号名称" });
  await user.clear(name);
  await user.type(name, "账号 B");
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  expect(screen.getByRole("alert")).toHaveTextContent("账号名称已存在");
  expect(update).not.toHaveBeenCalled();
});

it("后台刷新失败保留账号列表，并可重试恢复", async () => {
  accounts.current = [
    { id: "saved", name: "工作账号", authMode: "api-key", endpoints: [] },
  ];
  const user = userEvent.setup();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AIAccountsSection />
    </QueryClientProvider>,
  );
  await screen.findByRole("button", { name: "编辑账号 工作账号" });
  list.mockRejectedValueOnce(new Error("暂时离线"));
  await act(() => queryClient.invalidateQueries({ queryKey: ["accounts"] }));
  expect(await screen.findByRole("alert")).toHaveTextContent("暂时离线");
  expect(
    screen.getByRole("button", { name: "编辑账号 工作账号" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("button", { name: "编辑账号 工作账号" }),
  ).toBeVisible();
});

it("Google 服务商提供原生 GenerateContent 地址与 API Key 表单", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(await screen.findByRole("button", { name: "添加账号" }));
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(await screen.findByRole("option", { name: "Google" }));
  expect(screen.getByLabelText("API 地址")).toHaveValue(
    "https://generativelanguage.googleapis.com/v1beta",
  );
  expect(screen.getByLabelText("API Key")).toBeInTheDocument();
  expect(screen.queryByText("ChatGPT 订阅")).not.toBeInTheDocument();
});

it("编辑保留原生自定义推理 ID 并原样提交", async () => {
  accounts.current = [
    {
      id: "native",
      name: "原生账号",
      authMode: "api-key",
      revision: 1,
      hasCredential: true,
      endpoints: [
        { protocol: "openai-responses", baseUrl: "https://example.test/v1" },
      ],
      selectedModels: ["org/model"],
      modelCatalog: ["org/model"],
      modelReasoningLevels: {
        "org/model": ["LOW", "provider-fast", "budget:8192"],
      },
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await user.click(
    await screen.findByRole("button", { name: "编辑账号 原生账号" }),
  );
  expect(
    screen.getByRole("button", { name: "org/model 的思考等级" }),
  ).toHaveTextContent("LOW, provider-fast, budget:8192");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          modelReasoningLevels: {
            "org/model": ["LOW", "provider-fast", "budget:8192"],
          },
        }),
      }),
      expect.anything(),
    ),
  );
});
