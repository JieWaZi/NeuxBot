import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
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

vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listAiAccountsOptions: () => ({
    queryKey: ["accounts"],
    queryFn: async () => ({
      resources: [
        {
          id: "anthropic-account",
          name: "Anthropic 账号",
          selectedModels: ["anthropic-model"],
          endpoints: [
            {
              protocol: "anthropic-messages",
              baseUrl: "https://api.anthropic.com",
            },
          ],
        },
        {
          id: "deepseek-account",
          name: "DeepSeek",
          selectedModels: ["deepseek-flash"],
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.deepseek.com",
            },
          ],
        },
        {
          id: "gpt-subscription",
          name: "ChatGPT 订阅",
          authMode: "subscription",
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.openai.com/v1",
            },
          ],
        },
      ],
    }),
  }),
}));

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

const runtimes = ["kimi", "cursor", "pi", "claude"].map((id) => ({
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
}));

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
  const controls = useChatComposerControls({
    selectedAccountId: selection.accountId,
    activeChatId,
    projects: [],
    runtimes: availableRuntimes,
    runtimeId: selection.runtimeId,
    selectedRuntime: selected,
    selectedModel: selection.model,
    selectedModelName:
      selected?.models.find((model) => model.id === selection.model)?.name ??
      "",
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

it.each(["cursor", "pi"])(
  "普通与团队共用选择器支持 %s 和其模型",
  async (id) => {
    const user = userEvent.setup();
    const changed = vi.fn();
    renderControls(changed);
    await user.click(screen.getByRole("button", { name: "Kimi" }));
    await user.click(
      screen.getByRole("menuitemradio", {
        name: new RegExp(`^${id}\\s*可用$`, "i"),
      }),
    );
    if (id === "pi") {
      expect(
        screen.getByRole("menuitem", { name: "返回智能体列表" }),
      ).toBeVisible();
      await user.click(screen.getByRole("menuitemradio", { name: "内置账号" }));
    }
    await user.click(
      screen.getByRole("button", { name: new RegExp(`${id} Default`, "i") }),
    );
    await user.click(screen.getByRole("radio", { name: `${id} Model` }));
    await waitFor(() =>
      expect(changed).toHaveBeenLastCalledWith(
        expect.objectContaining({ model: `${id}/model` }),
      ),
    );
    expect(changed).toHaveBeenCalledWith(
      expect.objectContaining({ runtimeId: id, model: `${id}-default` }),
    );
    expect(
      screen.getByRole("button", { name: `${id} Model` }),
    ).toBeInTheDocument();
  },
);

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

it("代理菜单不展示未就绪代理和内部诊断", async () => {
  const user = userEvent.setup();
  const availableRuntimes = [
    ...runtimes,
    ...["Missing", "Broken"].map((name) => ({
      enabled: true,
      defaultModel: "missing-default",
      defaultReasoning: "medium",
      models: [],
      id: name,
      name,
      state: "unavailable",
      diagnostics: [{ message: "[RuntimeUnavailable][50000000006]" }],
    })),
  ];
  renderControls(vi.fn(), availableRuntimes);
  await user.click(screen.getByTestId("composer-runtime-control"));
  expect(
    screen.queryByRole("menuitemradio", { name: /Missing|Broken/ }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/RuntimeUnavailable/)).not.toBeInTheDocument();
  expect(screen.queryByText("Cursor CLI")).not.toBeInTheDocument();
  expect(
    screen.getByRole("menuitemradio", { name: /Cursor/ }),
  ).not.toHaveTextContent("CLI");
});

it("在智能体菜单内选择账号并重算模型与 Thinking", async () => {
  const user = userEvent.setup();
  const changed = vi.fn();
  renderControls(changed);
  expect(
    screen.queryByRole("button", { name: "账号" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Kimi" }));
  await user.click(
    screen.getByRole("menuitemradio", { name: /^Kimi\s*可用$/ }),
  );
  expect(
    screen.getByRole("menuitem", { name: "返回智能体列表" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("menuitemradio", { name: /^Pi\s*可用$/ }),
  ).not.toBeInTheDocument();
  await user.click(
    await screen.findByRole("menuitemradio", { name: "Anthropic 账号" }),
  );
  expect(changed).toHaveBeenLastCalledWith(
    expect.objectContaining({
      accountId: "anthropic-account",
      model: "anthropic-model",
      reasoning: "",
    }),
  );
  expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
    "Kimi · Anthropic 账号",
  );
  await user.click(screen.getByTestId("composer-runtime-control"));
  await user.click(screen.getByRole("menuitemradio", { name: /^Pi\s*可用$/ }));
  expect(changed.mock.lastCall?.[0]).toHaveProperty(
    "accountId",
    "anthropic-account",
  );
  await user.click(screen.getByRole("menuitemradio", { name: "内置账号" }));
  expect(changed.mock.lastCall?.[0]).toHaveProperty("accountId", "system");
  expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
    "Pi",
  );
});

it("仅有内置账号的智能体不展开账号，并只展示兼容账号", async () => {
  const user = userEvent.setup();
  renderControls(vi.fn());
  await user.click(screen.getByRole("button", { name: "Kimi" }));
  await user.click(
    screen.getByRole("menuitemradio", { name: /^claude\s*可用$/i }),
  );

  expect(
    screen.getByRole("menuitemradio", { name: "Anthropic 账号" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("menuitemradio", { name: /DeepSeek/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("menuitemradio", { name: /ChatGPT 订阅/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("menuitemradio", { name: /协议与当前智能体不兼容/ }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("menuitem", { name: "返回智能体列表" }));
  expect(
    screen.queryByRole("menuitemradio", { name: "Anthropic 账号" }),
  ).not.toBeInTheDocument();
  await user.click(
    screen.getByRole("menuitemradio", { name: /^cursor\s*可用$/i }),
  );
  expect(
    screen.queryByRole("menuitemradio", { name: "内置账号" }),
  ).not.toBeInTheDocument();
  expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
    "Cursor",
  );
});
