import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { AIAccountsSection } from "./ai-accounts-section";
import "@/shared/i18n";
import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption } from "@/shared/api/generated/types.gen";

const {
  create,
  update,
  preview,
  refresh,
  accounts,
  list,
  readCredential,
  logout,
  detectRuntime,
} = vi.hoisted(() => ({
  detectRuntime: vi.fn(async () => ({})),
  list: vi.fn(),
  create: vi.fn(async () => ({ id: "new-account" })),
  update: vi.fn(async () => ({})),
  logout: vi.fn(async () => ({})),
  preview: vi.fn(async () => ({
    resources: [] as Array<{ id: string; name: string }>,
  })),
  refresh: vi.fn(async () => ({
    resources: [] as Array<{ id: string; name: string }>,
  })),
  accounts: { current: [] as Array<Record<string, unknown>> },
  readCredential: vi.fn(async () => ({ data: { apiKey: "saved-secret" } })),
}));
vi.mock("./account-login", () => ({
  AccountLogin: ({ accountId }: { accountId: string }) => (
    <p>授权账号 {accountId}</p>
  ),
}));
vi.mock("./runtime-authentication-controls", () => ({
  RuntimeSystemAccountActions: () => null,
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getAiAccountCredential: readCredential,
  refreshRuntime: detectRuntime,
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({ queryKey: ["accounts"], queryFn: list }),
  listAiAccountsQueryKey: () => ["accounts"],
  listRuntimesQueryKey: () => ["runtimes"],
  createAiAccountMutation: () => ({ mutationFn: create }),
  updateAiAccountMutation: () => ({ mutationFn: update }),
  previewAiAccountModelsMutation: () => ({ mutationFn: preview }),
  refreshAiAccountModelsMutation: () => ({ mutationFn: refresh }),
  deleteAiAccountMutation: () => ({ mutationFn: vi.fn() }),
  logoutAiAccountMutation: () => ({ mutationFn: logout }),
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
  logout.mockClear();
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://api.example.com/v1",
      },
      selectedModels: ["model-a"],
      modelCatalog: ["model-a"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "密钥账号");
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
function renderAccounts(
  runtime: RuntimeOption = {
    id: "opencode",
    baseRuntimeId: "opencode",
    defaultAccountId: "",
  },
  onSelectAccount: (id: string) => Promise<void> = async () => {},
  disabled = false,
) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <AIAccountsSection
        runtime={runtime}
        onSelectAccount={onSelectAccount}
        disabled={disabled}
      />
    </QueryClientProvider>,
  );
}

it("每个账号只保存一个请求协议，API 地址不再选择格式", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
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
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://api.openai.com/v1",
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("内置服务商切换协议时同步地址，账号只保存当前协议", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
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
          endpoint: {
            protocol: "anthropic-messages",
            baseUrl: "https://api.deepseek.com/anthropic",
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("服务商选择框旁的官网按钮打开对应网站", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
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
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
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
      endpoint: {
        protocol: "openai-completions",
        baseUrl: "https://example.com/v1",
      },
      selectedModels: ["model-a"],
      modelCatalog: ["model-a"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "模型账号");
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
      endpoint: {
        protocol: "anthropic-messages",
        baseUrl: "https://example.com",
      },
      selectedModels: ["fast", "strong"],
      modelCatalog: ["fast", "strong"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "映射账号");
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://api.deepseek.com",
      },
      selectedModels: ["deepseek-flash"],
      modelCatalog: ["deepseek-flash"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "DeepSeek Responses");
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
      },
      selectedModels: ["doubao-seed-1-6-flash-250615"],
      modelCatalog: ["doubao-seed-1-6-flash-250615"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "火山方舟旧模型");
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://ark.cn-beijing.volces.com/api/v3",
      },
      selectedModels: ["doubao-seed-2-1-pro-260628"],
      modelCatalog: ["doubao-seed-2-1-pro-260628"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "火山方舟新模型");
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
      endpoint: {
        protocol: "anthropic-messages",
        baseUrl: "https://api.deepseek.com/anthropic",
      },
      selectedModels: ["deepseek-v4-pro", "deepseek-v4-flash"],
      modelCatalog: ["deepseek-v4-pro", "deepseek-v4-flash"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "DeepSeek Anthropic");
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
      endpoint: {
        protocol: "anthropic-messages",
        baseUrl: "https://api.anthropic.com",
      },
      selectedModels: ["claude-model"],
      modelCatalog: ["claude-model"],
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "账号 A");
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://old.example.com/v1",
      },
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "账号 A");
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

it("新增账号去除首尾空白后不能与现有账号重名", async () => {
  accounts.current = [
    {
      id: "saved",
      name: "OpenAI",
      authMode: "api-key",
      hasCredential: true,
      endpoint: undefined,
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await screen.findByRole("button", { name: "OpenAI 的账号操作" });
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
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
    endpoint: {
      protocol: "openai-responses",
      baseUrl: "https://api.openai.com/v1",
    },
  }));
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "账号 A");
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
    { id: "saved", name: "工作账号", authMode: "api-key", endpoint: undefined },
  ];
  const user = userEvent.setup();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AIAccountsSection
        runtime={{
          id: "opencode",
          baseRuntimeId: "opencode",
          defaultAccountId: "",
        }}
        onSelectAccount={async () => {}}
      />
    </QueryClientProvider>,
  );
  await screen.findByRole("button", { name: "工作账号 的账号操作" });
  list.mockRejectedValueOnce(new Error("暂时离线"));
  await act(() => queryClient.invalidateQueries({ queryKey: ["accounts"] }));
  expect(await screen.findByRole("alert")).toHaveTextContent("暂时离线");
  expect(
    screen.getByRole("button", { name: "工作账号 的账号操作" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("button", { name: "工作账号 的账号操作" }),
  ).toBeVisible();
});

it("Google 服务商提供原生 GenerateContent 地址与 API Key 表单", async () => {
  const user = userEvent.setup();
  renderAccounts();
  await user.click(await screen.findByRole("button", { name: "添加密钥" }));
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
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://example.test/v1",
      },
      selectedModels: ["org/model"],
      modelCatalog: ["org/model"],
      modelReasoningLevels: {
        "org/model": ["LOW", "provider-fast", "budget:8192"],
      },
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "原生账号");
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

async function openEdit(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(
    await screen.findByRole("button", { name: `${name} 的账号操作` }),
  );
  await user.click(
    await screen.findByRole("menuitem", { name: "更改 API Key" }),
  );
}

it("新增 API 账号携带详情智能体归属并仅提供兼容协议", async () => {
  const user = userEvent.setup();
  renderAccounts({ id: "claude", baseRuntimeId: "claude" });
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
  expect(screen.getByLabelText("API 地址")).toHaveValue(
    "https://api.anthropic.com",
  );
  await user.type(screen.getByLabelText("API Key"), "key");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          runtimeId: "claude",
          endpoint: {
            protocol: "anthropic-messages",
            baseUrl: "https://api.anthropic.com",
          },
        }),
      }),
      expect.anything(),
    ),
  );
});

it("Grok 的密钥配置默认使用自身官方服务，不误导用户填写到 OpenAI", async () => {
  renderAccounts({ id: "grok", baseRuntimeId: "grok" });
  await userEvent.click(screen.getByRole("button", { name: "添加密钥" }));
  expect(screen.getByRole("textbox", { name: "账号名称" })).toHaveValue("xAI");
  expect(screen.getByLabelText("API 地址")).toHaveValue("https://api.x.ai/v1");
});
it("订阅账号可在详情内重命名并保留原生默认模型", async () => {
  accounts.current = [
    {
      id: "subscription",
      name: "个人订阅",
      authMode: "subscription",
      hasCredential: true,
      defaultModel: "native-model",
      defaultReasoning: "high",
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://api.openai.com/v1",
      },
    },
  ];
  const user = userEvent.setup();
  renderAccounts({ id: "codex", baseRuntimeId: "codex" });
  await user.click(
    await screen.findByRole("button", { name: "个人订阅 的账号操作" }),
  );
  await user.click(await screen.findByRole("menuitem", { name: "重命名" }));
  const name = screen.getByRole("textbox", { name: "账号名称" });
  await user.clear(name);
  await user.type(name, "工作订阅");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          runtimeId: "codex",
          name: "工作订阅",
          defaultModel: "native-model",
          defaultReasoning: "high",
        }),
      }),
      expect.anything(),
    ),
  );
});
it("替换 API 默认模型清除原模型的推理强度", async () => {
  accounts.current = [
    {
      id: "api",
      name: "测试账号",
      authMode: "api-key",
      hasCredential: true,
      defaultModel: "old-model",
      defaultReasoning: "high",
      selectedModels: ["new-model"],
      modelCatalog: ["new-model"],
      modelReasoningLevels: { "new-model": ["low"] },
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://api.openai.com/v1",
      },
    },
  ];
  const user = userEvent.setup();
  renderAccounts();
  await openEdit(user, "测试账号");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          defaultModel: "new-model",
          defaultReasoning: "",
        }),
      }),
      expect.anything(),
    ),
  );
});
it("当前账号删除入口禁用，切换使用真实默认账号操作", async () => {
  accounts.current = [
    {
      id: "api",
      name: "当前工作账号",
      authMode: "api-key",
      hasCredential: true,
      endpoint: undefined,
    },
  ];
  const user = userEvent.setup();
  const select = vi.fn(async () => {});
  renderAccounts({ id: "codex", defaultAccountId: "api" }, select);
  await user.click(
    await screen.findByRole("button", { name: "当前工作账号 的账号操作" }),
  );
  expect(await screen.findByRole("menuitem", { name: "删除" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(screen.getByRole("menuitem", { name: "删除" })).toHaveClass(
    "text-destructive",
    "data-[highlighted]:text-destructive",
  );
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "切换" }));
  expect(select).toHaveBeenCalledWith("");
});

it("添加密钥使用分组标题右侧加号，不再显示重复的 API Key 摘要行", async () => {
  renderAccounts();
  const add = screen.getByRole("button", { name: "添加密钥" });
  expect(add.textContent).toBe("");
  expect(add.querySelector("svg")).toHaveClass("lucide-plus");
  expect(
    screen.getByRole("heading", { name: "API 密钥" }).parentElement
      ?.parentElement,
  ).toContainElement(add);
  expect(
    screen.queryByRole("heading", { name: "API Key" }),
  ).not.toBeInTheDocument();
});

it("模型请求未结束时固定端点身份，完成后切换服务商不会混入旧模型", async () => {
  let resolveModels:
    | ((result: { resources: Array<{ id: string; name: string }> }) => void)
    | undefined;
  preview.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveModels = resolve;
      }),
  );
  const user = userEvent.setup();
  renderAccounts();
  await user.click(screen.getByRole("button", { name: "添加密钥" }));
  await user.type(screen.getByLabelText("API Key"), "key");
  await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
  expect(screen.getByRole("combobox", { name: "服务商" })).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "请求协议" })).toBeDisabled();
  expect(screen.getByLabelText("API 地址")).toBeDisabled();
  expect(screen.getByLabelText("API Key")).toBeDisabled();
  await act(async () =>
    resolveModels?.({ resources: [{ id: "old-model", name: "Old Model" }] }),
  );
  await user.click(screen.getByRole("combobox", { name: "服务商" }));
  await user.click(await screen.findByRole("option", { name: "Anthropic" }));
  expect(screen.getByLabelText("API 地址")).toHaveValue(
    "https://api.anthropic.com",
  );
  await user.click(screen.getByRole("button", { name: "选择模型" }));
  expect(screen.queryByText("Old Model")).not.toBeInTheDocument();
});

it("Claude 详情提供添加订阅账号并创建 Anthropic 归属配置", async () => {
  const user = userEvent.setup();
  renderAccounts({ id: "claude", baseRuntimeId: "claude" });
  const add = screen.getByRole("button", { name: "添加账号" });
  expect(add).not.toHaveTextContent("添加账号");
  await user.click(add);
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          runtimeId: "claude",
          authMode: "subscription",
          endpoint: {
            protocol: "anthropic-messages",
            baseUrl: "https://api.anthropic.com",
          },
        }),
      }),
      expect.anything(),
    ),
  );
  expect(await screen.findByText("授权账号 new-account")).toBeVisible();
});

it("安装期间不能从现有订阅账号启动登录", async () => {
  accounts.current = [
    {
      id: "subscription",
      name: "待登录订阅",
      authMode: "subscription",
      hasCredential: false,
      endpoint: undefined,
    },
  ];
  const user = userEvent.setup();
  renderAccounts(
    { id: "claude", baseRuntimeId: "claude" },
    async () => {},
    true,
  );
  const login = await screen.findByRole("button", { name: "登录" });
  expect(login).toBeDisabled();
  await user.click(login);
  expect(screen.queryByText("授权账号 subscription")).not.toBeInTheDocument();
});

it.each([
  ["missing", "未登录"],
  ["ok", "已连接（原生客户端未提供账号名称）"],
  ["unknown", "登录状态未确认"],
])("内置账号选择不冒充登录状态：%s", async (auth, label) => {
  renderAccounts({ id: "kimi", baseRuntimeId: "kimi", installed: true, auth });
  expect(await screen.findByText(label)).toBeVisible();
  expect(screen.getByText("当前账号")).toBeVisible();
});
