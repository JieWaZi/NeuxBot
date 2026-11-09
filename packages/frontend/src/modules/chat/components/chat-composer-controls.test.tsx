import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { afterAll, beforeAll, expect, it, vi } from "vitest";
import { usesNativeModelCatalog } from "@/shared/api/runtime-queries";
import "@/shared/i18n";

const { listWorkspaceGitRepositories } = vi.hoisted(() => ({
  listWorkspaceGitRepositories: vi.fn(async () => ({
    repositories: [
      {
        directory: "/tmp/source-repo",
        gitRoot: "/tmp/source-repo",
        label: "source-repo",
        branch: "main",
      },
    ],
  })),
}));

vi.mock("@/shared/platform", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    isDesktopRuntime: () => true,
    hasPlatformService: () => true,
    listWorkspaceGitRepositories,
    listWorkspaceGitBranches: vi.fn(async () => ({
      isGitRepo: true,
      branches: [{ name: "main", isCurrent: true, isRemote: false }],
    })),
    previewWorkspaceGitCheckout: vi.fn(async () => ({
      canCheckout: true,
      conflicts: [],
    })),
    checkoutWorkspaceGitBranch: vi.fn(async () => ({ branch: "main" })),
  };
});
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  useChatComposerControls,
  type ChatExecutionSelectionPatch,
} from "./chat-composer-controls";
import { ChatExecutionPicker } from "./chat-execution-picker";

vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({
    queryKey: ["accounts"],
    queryFn: async () => ({
      resources: [
        {
          id: "anthropic-account",
          name: "Anthropic 账号",
          selectedModels: ["anthropic-model"],
          endpoint: {
            protocol: "anthropic-messages",
            baseUrl: "https://api.anthropic.com",
          },
        },
        {
          id: "deepseek-account",
          name: "DeepSeek",
          selectedModels: ["deepseek-flash"],
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://api.deepseek.com",
          },
        },
        {
          id: "gpt-subscription",
          name: "ChatGPT 订阅",
          authMode: "subscription",
          endpoint: {
            protocol: "openai-responses",
            baseUrl: "https://api.openai.com/v1",
          },
        },
      ],
    }),
  }),
}));

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: ({
    runtime,
  }: {
    runtime?: (typeof runtimes)[number];
  }) => ({
    models: runtime?.models ?? [],
    defaultModel: runtime?.defaultModel,
    catalogState: "fresh",
    catalogPending: false,
  }),
}));

beforeAll(() => {
  Element.prototype.hasPointerCapture = () => false;
});
afterAll(() => {
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
});

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderControls(
  onChange: (patch: ChatExecutionSelectionPatch) => void,
  availableRuntimes = runtimes,
  activeChatId?: string,
  gitWorkspaceDirectories?: string[],
) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={0}>
          <Controls
            activeChatId={activeChatId}
            gitWorkspaceDirectories={gitWorkspaceDirectories}
            onChange={onChange}
            availableRuntimes={availableRuntimes}
          />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const runtimes = ["kimi", "cursor", "pi", "claude", "codex", "workbuddy"].map(
  (id) => ({
    id,
    name: id === "cursor" ? "Cursor CLI" : id,
    enabled: true,
    state: "ready",
    defaultModel: `${id}-default`,
    defaultReasoning: "medium",
    models: [
      { id: `${id}-default`, name: `${id} Default` },
      { id: `${id}/model`, name: `${id} Model` },
    ],
  }),
);

function Controls({
  activeChatId,
  gitWorkspaceDirectories,
  onChange,
  availableRuntimes = runtimes,
}: {
  activeChatId?: string;
  gitWorkspaceDirectories?: string[];
  onChange: (patch: ChatExecutionSelectionPatch) => void;
  availableRuntimes?: typeof runtimes;
}) {
  const [selection, setSelection] = useState({
    accountId: "system",
    runtimeId: "kimi",
    model: "kimi-default",
  });
  const selected = runtimes.find(
    (runtime) => runtime.id === selection.runtimeId,
  );
  const nativeCatalog = usesNativeModelCatalog(selected?.id);
  const model = selection.model || selected?.defaultModel || "";
  const controls = useChatComposerControls({
    catalogState: nativeCatalog ? "fresh" : undefined,
    modelOptionsOverride: nativeCatalog ? selected?.models : undefined,
    selectedAccountId: selection.accountId,
    activeChatId,
    projects: [],
    runtimes: availableRuntimes,
    runtimeId: selection.runtimeId,
    selectedRuntime: selected,
    selectedModel: model,
    selectedModelName:
      selected?.models.find((option) => option.id === model)?.name ?? "",
    selectedPermissionLevel: "default",
    supportedPermissions: new Set(["default"]),
    selectedReasoning: "medium",
    historicalRuntimeDisabled: false,
    gitWorkspaceDirectories,
    workspaceDirectory: "/managed/chat-workspace",
    workspaceLabel: "Work",
    onPickDirectory: () => {},
    onSelectProject: () => {},
    updateExecutionSelection: (patch) => {
      onChange(patch);
      setSelection((current) => ({ ...current, ...patch }));
    },
  });
  return (
    <>
      {controls.renderContextStrip()}
      {controls.composerSettings}
    </>
  );
}

it.each(["codex", "claude", "cursor", "pi", "workbuddy"])(
  "预览 %s 目录后一次提交智能体、账号与模型",
  async (id) => {
    const user = userEvent.setup();
    const changed = vi.fn();
    renderControls(changed);
    await user.click(screen.getByTestId("composer-model-control"));
    await user.click(
      screen.getByRole("button", { name: new RegExp(`^${id}$`, "i") }),
    );
    expect(changed).not.toHaveBeenCalled();
    await user.type(
      screen.getByRole("searchbox", { name: "搜索模型" }),
      "/model",
    );
    expect(
      screen.queryByRole("radio", { name: `${id} Default` }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: `${id} Model` }));
    expect(changed).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        runtimeId: id,
        accountId: "system",
        model: `${id}/model`,
      }),
    );
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      `${id} Model`,
    );
    expect(screen.getByRole("dialog", { name: "选择模型" })).toBeVisible();
    expect(screen.getByRole("radio", { name: `${id} Model` })).toBeChecked();
  },
);

it("目录刷新失败时用感叹号提示并允许选择已有模型", async () => {
  const user = userEvent.setup();
  const onModel = vi.fn();
  const models = [
    { id: "cursor-first", name: "Cursor First" },
    { id: "cursor-second", name: "Cursor Second" },
  ];
  const runtime = { id: "cursor", enabled: true, state: "ready", models };
  render(
    <ChatExecutionPicker
      runtimes={[runtime]}
      accounts={[]}
      runtime={runtime}
      accountId="system"
      models={models}
      modelId="cursor-first"
      modelName="Cursor First"
      reasoning=""
      allowDefaultReasoning
      catalogState="stale"
      catalogIssue="catalogStale"
      onRefresh={vi.fn()}
      onModel={onModel}
      onReasoning={vi.fn()}
      onFast={vi.fn()}
    />,
  );
  await user.click(screen.getByTestId("composer-model-control"));
  expect(screen.getByRole("status")).toHaveTextContent("模型目录刷新失败");
  expect(screen.queryByText(/已保留上次结果/)).not.toBeInTheDocument();
  const option = screen.getByRole("radio", { name: "Cursor Second" });
  expect(option).toBeEnabled();
  await user.click(option);
  expect(onModel).toHaveBeenCalledExactlyOnceWith(runtime, "system", models[1]);
});

it("已有对话时用全部源目录查询 Git 仓库", async () => {
  listWorkspaceGitRepositories.mockClear();
  renderControls(vi.fn(), runtimes, "chat-1", [
    "/tmp/source-repo",
    "/tmp/other-repo",
  ]);

  await waitFor(() =>
    expect(listWorkspaceGitRepositories).toHaveBeenCalledWith([
      "/tmp/source-repo",
      "/tmp/other-repo",
    ]),
  );
  await waitFor(() =>
    expect(
      screen.getByTestId("composer-workspace-control"),
    ).toBeInTheDocument(),
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-branch-control")).toHaveTextContent(
      "main",
    ),
  );
});

it("已有对话时锁定工作区选择并提示开启新对话", async () => {
  const user = userEvent.setup();
  renderControls(vi.fn(), runtimes, "chat-1");

  const control = screen.getByTestId("composer-workspace-control");
  expect(control.tagName).toBe("SPAN");
  await user.click(control);
  expect(screen.queryByText("本地目录")).not.toBeInTheDocument();

  expect(control).toHaveAttribute("title", "如需修改目录请开启新对话");
});

it("目录保留探测失败的智能体并只展示兼容账号", async () => {
  const user = userEvent.setup();
  const availableRuntimes = [
    ...runtimes,
    {
      id: "missing",
      name: "Missing",
      enabled: true,
      state: "unavailable",
      defaultModel: "",
      defaultReasoning: "",
      models: [],
    },
  ];
  const changed = vi.fn();
  renderControls(changed, availableRuntimes);
  await user.click(screen.getByTestId("composer-model-control"));
  expect(screen.getByRole("button", { name: "Missing" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: /^claude$/i }));
  await user.click(screen.getByRole("combobox", { name: "账号" }));
  expect(
    screen.queryByRole("option", { name: "DeepSeek" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("option", { name: "Anthropic 账号" }));
  expect(changed).not.toHaveBeenCalled();
  await user.click(screen.getByRole("radio", { name: "claude Model" }));
  expect(changed).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      runtimeId: "claude",
      accountId: "anthropic-account",
      model: "claude/model",
    }),
  );
});

it("模型行展示已知的上下文档位，并将 OpenCode 免费模型放到前面", async () => {
  const user = userEvent.setup();
  const changed = vi.fn();
  const fixtures = [
    ...runtimes.map((runtime) =>
      runtime.id === "cursor"
        ? {
            ...runtime,
            models: [
              {
                id: "gpt-5.6-sol",
                name: "GPT-5.6 Sol",
                contextWindowTokens: 272_000,
                maxContextWindowTokens: 1_000_000,
                defaultReasoning: "medium",
                reasoningOptions: [{ id: "medium", name: "Medium" }],
              },
              { id: "gemini[reasoning=high]", name: "Gemini" },
            ],
          }
        : runtime,
    ),
    {
      id: "opencode",
      name: "OpenCode",
      enabled: true,
      state: "ready",
      defaultModel: "openai/paid",
      defaultReasoning: "",
      models: [
        { id: "openai/paid", name: "Paid Model" },
        { id: "opencode/free", name: "Beam Free" },
      ],
    },
  ];
  renderControls(changed, fixtures);
  await user.click(screen.getByTestId("composer-model-control"));
  await user.click(screen.getByRole("button", { name: "Cursor" }));
  expect(screen.getByRole("radio", { name: "GPT-5.6 Sol" })).toHaveTextContent(
    "272K / 1M · 中",
  );
  expect(screen.getByRole("radio", { name: "Gemini" })).toHaveTextContent("高");
  await user.click(screen.getByRole("button", { name: "OpenCode" }));
  const rows = screen.getAllByRole("radio");
  expect(rows[0]).toHaveTextContent("Beam Free免费");
  expect(rows[1]).toHaveTextContent("Paid Model默认");
  expect(changed).not.toHaveBeenCalled();
});

it("Qoder 可选两份目录的模型，只展示新模型和自定义分组", async () => {
  const user = userEvent.setup();
  const changed = vi.fn();
  const fixtures = [
    ...runtimes,
    {
      id: "qoder",
      name: "Qoder",
      enabled: true,
      state: "ready",
      defaultModel: "qfmodel",
      defaultReasoning: "",
      models: [
        {
          id: "qfmodel",
          name: "Qwen3.8-Flash",
          group: "qoder/new",
          free: true,
          creditMultiplier: 0,
        },
        { id: "auto", name: "Auto", group: "qoder/default" },
        {
          id: "smodel",
          name: "Sonus",
          group: "qoder/new",
          creditMultiplier: 8,
        },
        { id: "custom-model", name: "Custom Model", group: "qoder/custom" },
      ],
    },
  ];
  renderControls(changed, fixtures);
  await user.click(screen.getByTestId("composer-model-control"));
  await user.click(screen.getByRole("button", { name: "Qoder" }));
  const sonus = screen.getByRole("radio", { name: "Sonus" });
  expect(screen.queryByRole("radio", { name: "Auto" })).not.toBeInTheDocument();
  expect(screen.getByRole("group", { name: "新模型" })).toContainElement(sonus);
  expect(sonus).toHaveTextContent("8×");
  const custom = screen.getByRole("radio", { name: "Custom Model" });
  expect(screen.getByRole("group", { name: "自定义" })).toContainElement(
    custom,
  );
  expect(custom).toBeEnabled();
  const flash = screen.getByRole("radio", { name: "Qwen3.8-Flash" });
  expect(flash).toHaveTextContent("免费");
  expect(flash).toHaveTextContent("0×");
  await user.click(sonus);
  expect(changed).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({ runtimeId: "qoder", model: "smodel" }),
  );
});
