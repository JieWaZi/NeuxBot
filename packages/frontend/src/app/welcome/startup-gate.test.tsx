import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type {
  UpdateRuntimeEnvironmentData,
  ListRuntimesResponse,
} from "@/shared/api/generated/types.gen";
import "@/shared/i18n";
import { StartupGate } from "./startup-gate";
import { cliInstallation } from "./cli-installation";

const api = vi.hoisted(() => ({
  read: vi.fn(),
  refresh: vi.fn(),
  refreshOne: vi.fn(),
  update: vi.fn(),
}));
vi.mock("@/shared/api/runtime-queries", () => ({
  listRuntimesOptions: () => ({ queryKey: ["runtimes"], queryFn: api.read }),
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  refreshRuntimesMutation: () => ({ mutationFn: api.refresh }),
  refreshRuntimeMutation: () => ({ mutationFn: api.refreshOne }),
  updateRuntimeEnvironmentMutation: () => ({ mutationFn: api.update }),
}));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
vi.mock("./welcome-brand", () => ({
  WelcomeBrand: ({ children }: { children?: ReactNode }) => (
    <main>
      <div role="img" aria-label="NeuxBot" />
      {children}
    </main>
  ),
}));

const missing: ListRuntimesResponse = {
  startupDetectionPending: false,
  startupDetectionFailed: false,
  startupSetupRequired: true,
  resources: [
    { id: "codex", baseRuntimeId: "codex", name: "Codex", installed: false },
    {
      id: "opencode",
      baseRuntimeId: "opencode",
      name: "OpenCode",
      installed: false,
    },
    { id: "grok", baseRuntimeId: "grok", name: "Grok Build", installed: false },
  ],
};
const found: ListRuntimesResponse = {
  ...missing,
  resources: [
    {
      id: "opencode",
      baseRuntimeId: "opencode",
      name: "OpenCode",
      installed: true,
      state: "ready",
      detectedAt: 1,
      binary: "/usr/local/bin/opencode",
      prefixArgs: ["--debug"],
      configHome: "/custom/home",
      defaultPermissionLevel: "auto",
      defaultModel: "free-model",
      models: [
        {
          id: "free-model",
          permissionLevels: ["default", "auto", "full-access"],
        },
      ],
      version: "2.0.0",
      auth: "missing",
    },
  ],
};
const multipleVersions: ListRuntimesResponse = {
  ...found,
  resources: [
    {
      ...found.resources?.[0],
      isDefault: true,
      resolvedBinary: "/usr/local/bin/opencode",
      installations: [
        {
          path: "/usr/local/bin/opencode",
          version: "1.2.15",
          available: true,
        },
        {
          path: "/home/test/.opencode/bin/opencode",
          version: "opencode v2.0.29",
          available: true,
        },
      ],
    },
  ],
};
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  render(
    <QueryClientProvider client={client}>
      <StartupGate>
        <div>工作区</div>
      </StartupGate>
    </QueryClientProvider>,
  );
  return client;
}
afterEach(() => vi.unstubAllGlobals());
beforeEach(() => {
  vi.clearAllMocks();
  api.update.mockReset();
  api.read.mockResolvedValue(missing);
  api.refresh.mockResolvedValue(undefined);
  api.refreshOne.mockReset();
  api.refreshOne.mockResolvedValue(undefined);
  api.update.mockImplementation(
    async (variables: UpdateRuntimeEnvironmentData) => {
      if (variables.body?.isDefault) {
        const snapshot: ListRuntimesResponse = await api.read();
        api.read.mockResolvedValue({
          ...snapshot,
          startupSetupRequired: false,
        });
      }
    },
  );
});

it("首次探测时即可选择 CLI，各项保留检测中状态直到自身完成", async () => {
  api.read.mockResolvedValue({ ...missing, startupDetectionPending: true });
  const client = mount();
  await waitFor(() => expect(api.read).toHaveBeenCalledTimes(1));
  expect(screen.getByRole("img", { name: "NeuxBot" })).toBeInTheDocument();
  expect(
    await screen.findByRole("heading", { name: "设置智能体" }),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Codex" }),
  ).toHaveAccessibleDescription("正在检测…");
  fireEvent.click(screen.getByRole("button", { name: "Codex" }));
  expect(screen.getByRole("button", { name: "Codex" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByText("npm install -g @openai/codex")).toBeVisible();
  expect(api.refresh).not.toHaveBeenCalled();
  act(() =>
    client.setQueryData(["runtimes"], {
      ...missing,
      startupDetectionPending: true,
      resources: missing.resources?.map((runtime) =>
        runtime.id === "codex" ? { ...runtime, detectedAt: 1 } : runtime,
      ),
    }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Codex" }),
    ).toHaveAccessibleDescription("未安装"),
  );
  expect(screen.getByRole("button", { name: "重新检测" })).toBeEnabled();
  expect(
    screen.getByRole("button", { name: "Grok Build" }),
  ).toHaveAccessibleDescription("正在检测…");
  act(() => client.setQueryData(["runtimes"], missing));
  expect(
    await screen.findByRole("heading", { name: "设置智能体" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: /稍后|跳过|进入/ }),
  ).not.toBeInTheDocument();
});

it("欢迎期间已预取准入快照时，首次挂载直接进入，不插入第二张欢迎页", async () => {
  api.read.mockResolvedValue({ ...found, startupSetupRequired: false });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 10_000 } },
  });
  await client.prefetchQuery({ queryKey: ["runtimes"], queryFn: api.read });
  render(
    <QueryClientProvider client={client}>
      <StartupGate>
        <div>工作区</div>
      </StartupGate>
    </QueryClientProvider>,
  );
  expect(screen.getByText("工作区")).toBeVisible();
  expect(
    screen.queryByRole("img", { name: "NeuxBot" }),
  ).not.toBeInTheDocument();
  expect(api.read).toHaveBeenCalledOnce();
});

it("已有安装证据时自动进入，后续后台变化不抢走工作区", async () => {
  api.read.mockResolvedValue({
    ...found,
    startupSetupRequired: false,
    startupDetectionPending: true,
  });
  const client = mount();
  expect(await screen.findByText("工作区")).toBeVisible();
  act(() => client.setQueryData(["runtimes"], missing));
  expect(screen.getByText("工作区")).toBeVisible();
  expect(api.refresh).not.toHaveBeenCalled();
});

it("新用户检测到 CLI 后必须保存默认选择，保留原配置且不强制登录", async () => {
  api.read.mockResolvedValue(found);
  mount();
  expect(
    await screen.findByRole("heading", { name: "设置智能体" }),
  ).toBeVisible();
  fireEvent.click(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByText("工作区")).toBeVisible();
  expect(api.update).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { runtimeId: "opencode" },
      body: expect.objectContaining({
        enabled: true,
        isDefault: true,
        binary: "/usr/local/bin/opencode",
        prefixArgs: ["--debug"],
        configHome: "/custom/home",
        defaultModel: "free-model",
        defaultPermissionLevel: "auto",
      }),
    }),
    expect.anything(),
  );
});

it("多个安装版本必须显式选择，不能直接沿用 PATH 结果进入", async () => {
  api.read.mockResolvedValue(multipleVersions);
  const user = userEvent.setup();
  mount();
  const versions = await screen.findByRole("combobox", { name: "版本" });
  expect(versions).toHaveTextContent("请选择版本");
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeDisabled();
  versions.focus();
  await user.keyboard("{Enter}");
  expect(
    screen.getByRole("option", {
      name: "1.2.15 · /usr/local/bin/opencode",
    }),
  ).toBeVisible();
  expect(
    screen.getByRole("option", {
      name: "v2.0.29 · /home/test/.opencode/bin/opencode",
    }),
  ).toBeVisible();
  await user.keyboard("{Enter}");
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeEnabled();
  expect(api.update).not.toHaveBeenCalled();
});

it("切换版本后等待重新探测，最后保存新版本的模型与权限再进入", async () => {
  api.read.mockResolvedValue(multipleVersions);
  const user = userEvent.setup();
  let finishDetection!: () => void;
  api.update.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finishDetection = () => {
          api.read.mockResolvedValue({
            ...multipleVersions,
            resources: [
              {
                ...multipleVersions.resources?.[0],
                binary: "/home/test/.opencode/bin/opencode",
                resolvedBinary: "/home/test/.opencode/bin/opencode",
                version: "opencode v2.0.29",
                defaultModel: "new-free-model",
                defaultPermissionLevel: "default",
                models: [
                  { id: "new-free-model", permissionLevels: ["default"] },
                ],
              },
            ],
          });
          resolve();
        };
      }),
  );
  mount();
  const versions = await screen.findByRole("combobox", { name: "版本" });
  versions.focus();
  await user.keyboard("{Enter}{ArrowDown}{Enter}");
  await waitFor(() => expect(api.update).toHaveBeenCalledOnce());
  expect(api.update).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { runtimeId: "opencode" },
      body: expect.objectContaining({
        binary: "/home/test/.opencode/bin/opencode",
        isDefault: false,
        prefixArgs: ["--debug"],
        configHome: "/custom/home",
      }),
    }),
    expect.anything(),
  );
  expect(versions).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeDisabled();
  expect(screen.queryByText("工作区")).not.toBeInTheDocument();
  await act(async () => finishDetection());
  await waitFor(() => expect(versions).toBeEnabled());
  expect(screen.queryByText("工作区")).not.toBeInTheDocument();
  await user.click(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByText("工作区")).toBeVisible();
  expect(api.update).toHaveBeenLastCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({
        binary: "/home/test/.opencode/bin/opencode",
        isDefault: true,
        defaultModel: "new-free-model",
        defaultPermissionLevel: "default",
      }),
    }),
    expect.anything(),
  );
});

it("版本切换失败保留错误与引导，可以重新选择重试", async () => {
  api.read.mockResolvedValue(multipleVersions);
  api.update.mockRejectedValueOnce(new Error("版本切换失败"));
  const user = userEvent.setup();
  mount();
  const versions = await screen.findByRole("combobox", { name: "版本" });
  versions.focus();
  await user.keyboard("{Enter}{ArrowDown}{Enter}");
  expect(await screen.findByRole("alert")).toHaveTextContent("版本切换失败");
  expect(versions).toHaveTextContent("请选择版本");
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeDisabled();
  versions.focus();
  await user.keyboard("{Enter}{Enter}");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeEnabled();
});

it("单个安装版本不增加选择字段，模型目录失败时禁止进入并支持重检恢复", async () => {
  api.read.mockResolvedValue({
    ...found,
    resources: [
      {
        ...found.resources?.[0],
        installations: [
          {
            path: "/usr/local/bin/opencode",
            version: "1.2.15",
            available: true,
          },
        ],
        models: [],
        diagnostics: [{ code: "ACP_OPTIONS_UNAVAILABLE", level: "warning" }],
      },
    ],
  });
  mount();
  expect(
    await screen.findByRole("heading", { name: "设置智能体" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("combobox", { name: "版本" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent(
    "模型与权限配置尚未就绪",
  );
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeDisabled();
  api.refreshOne.mockImplementationOnce(async () =>
    api.read.mockResolvedValue(found),
  );
  fireEvent.click(screen.getByRole("button", { name: "重新检测" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
    ).toBeEnabled(),
  );
});

it.each([
  { models: [] },
  { defaultModel: "removed-model" },
  { models: [{ id: "free-model", permissionLevels: ["default"] }] },
  { diagnostics: [{ code: "ACP_OPTIONS_UNAVAILABLE" }] },
])(
  "模型为空、默认模型失效、权限不匹配或刷新失败均不能进入：%j",
  async (patch) => {
    api.read.mockResolvedValue({
      ...found,
      resources: [{ ...found.resources?.[0], ...patch }],
    });
    mount();
    expect(
      await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
    ).toBeDisabled();
    expect(api.update).not.toHaveBeenCalled();
  },
);

it("所选 CLI 已就绪时可直接保存进入，无需等待其他 CLI 探测完成", async () => {
  api.read.mockResolvedValue({ ...found, startupDetectionPending: true });
  mount();
  expect(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeEnabled();
  expect(screen.getByRole("status")).toHaveTextContent("已识别 1 个模型");
  fireEvent.click(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByText("工作区")).toBeVisible();
});

it("切换 CLI 后只重检当前项，各项重检并发且不会锁住其他 CLI", async () => {
  const snapshot: ListRuntimesResponse = {
    ...found,
    resources: [
      { ...found.resources?.[0], models: [] },
      {
        ...found.resources?.[0],
        id: "codex",
        baseRuntimeId: "codex",
        name: "Codex",
        defaultModel: "codex-model",
        models: [{ id: "codex-model", permissionLevels: ["auto"] }],
      },
    ],
  };
  api.read.mockResolvedValue(snapshot);
  const completions = new Map<string, () => void>();
  api.refreshOne.mockImplementation(
    ({ path }: { path: { runtimeId: string } }) =>
      new Promise<void>((resolve) => completions.set(path.runtimeId, resolve)),
  );
  mount();
  fireEvent.click(await screen.findByRole("button", { name: "重新检测" }));
  await waitFor(() => expect(completions.has("opencode")).toBe(true));
  expect(screen.getByRole("button", { name: "OpenCode" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Codex" }));
  expect(screen.getByRole("button", { name: "模型" })).toHaveTextContent(
    "codex-model",
  );
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "重新检测" }));
  await waitFor(() => expect(completions.has("codex")).toBe(true));
  expect(api.refreshOne).toHaveBeenNthCalledWith(
    1,
    { path: { runtimeId: "opencode" } },
    expect.anything(),
  );
  expect(api.refreshOne).toHaveBeenNthCalledWith(
    2,
    { path: { runtimeId: "codex" } },
    expect.anything(),
  );
  expect(api.refresh).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "OpenCode" }));
  expect(screen.getByRole("button", { name: "正在检测…" })).toBeDisabled();
  api.read.mockResolvedValue({
    ...snapshot,
    resources: [found.resources?.[0] ?? {}, snapshot.resources?.[1] ?? {}],
  });
  await act(async () => completions.get("opencode")?.());
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "重新检测" })).toBeEnabled(),
  );
  expect(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeEnabled();
  expect(screen.getByRole("button", { name: "模型" })).toHaveTextContent(
    "free-model",
  );
  fireEvent.click(screen.getByRole("button", { name: "Codex" }));
  expect(screen.getByRole("button", { name: "正在检测…" })).toBeDisabled();
  await act(async () => completions.get("codex")?.());
});

it("其他 CLI 探测失败时，已就绪的所选 CLI 仍可完成设置", async () => {
  api.read.mockResolvedValue({ ...found, startupDetectionFailed: true });
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByText("工作区")).toBeVisible();
});

it("保存后对账发现模型目录失效时仍停留引导", async () => {
  api.read.mockResolvedValue(found);
  api.update.mockImplementationOnce(async () => {
    api.read.mockResolvedValue({
      ...found,
      resources: [{ ...found.resources?.[0], models: [] }],
    });
  });
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "模型与权限配置尚未就绪",
  );
  expect(screen.queryByText("工作区")).not.toBeInTheDocument();
});

it("用户安装后手动重检，再设置默认进入", async () => {
  mount();
  expect(
    await screen.findByText("curl -fsSL https://opencode.ai/v2/install | bash"),
  ).toBeVisible();
  api.refreshOne.mockImplementation(async () => {
    api.read.mockResolvedValue(found);
  });
  fireEvent.click(screen.getByRole("button", { name: "重新检测" }));
  expect(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  ).toBeVisible();
  expect(api.refreshOne).toHaveBeenCalledWith(
    { path: { runtimeId: "opencode" } },
    expect.anything(),
  );
  expect(api.refresh).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByText("工作区")).toBeVisible();
});

it("探测故障与未安装分开，成功重检可恢复安装引导", async () => {
  api.read.mockResolvedValue({ ...missing, startupDetectionFailed: true });
  mount();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "本次检测未能完整完成",
  );
  expect(screen.queryByText("设置智能体")).not.toBeInTheDocument();
  api.refresh.mockImplementationOnce(async () =>
    api.read.mockResolvedValue(missing),
  );
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(
    await screen.findByRole("heading", { name: "设置智能体" }),
  ).toBeVisible();
});

it("保存失败保留引导与错误，不提前进入", async () => {
  api.read.mockResolvedValue(found);
  api.update.mockRejectedValue(new Error("默认设置保存失败"));
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "默认设置保存失败",
  );
  expect(screen.queryByText("工作区")).not.toBeInTheDocument();
});

it("安装目录覆盖全部内置 CLI，Windows 不提供 Unix 安装命令", () => {
  for (const id of [
    "opencode",
    "codex",
    "claude",
    "kimi",
    "gemini",
    "cursor",
    "pi",
    "grok",
    "workbuddy",
    "qoder",
    "qoder-cn",
  ]) {
    expect(cliInstallation(id, "macos")?.url).toMatch(/^https:\/\//);
    expect(cliInstallation(id, "windows")?.command ?? "").not.toMatch(
      /\| (?:bash|sh)$/,
    );
  }
  expect(cliInstallation("opencode", "windows")?.command).toBeUndefined();
});

it("Windows 引导显示 PowerShell 安装命令，复制内容与所选智能体一致", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", {
    userAgent: "Windows NT 10.0",
    platform: "Win32",
    clipboard: { writeText },
  });
  mount();
  expect(await screen.findByText(/请通过官方指南下载/)).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制安装命令" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /Grok Build/ }));
  expect(screen.getByText("Windows · 请在 PowerShell 中执行。")).toBeVisible();
  expect(
    screen.getByText("irm https://x.ai/cli/install.ps1 | iex"),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "复制安装命令" }));
  await waitFor(() =>
    expect(writeText).toHaveBeenCalledWith(
      "irm https://x.ai/cli/install.ps1 | iex",
    ),
  );
});

it("本次探测中连接失败，仍允许手动重试读取", async () => {
  api.read.mockResolvedValue({ ...missing, startupDetectionPending: true });
  const client = mount();
  await waitFor(() => expect(api.read).toHaveBeenCalledOnce());
  api.read.mockRejectedValue(new Error("连接失败"));
  await act(async () => {
    await client.invalidateQueries({ queryKey: ["runtimes"] });
  });
  expect(await screen.findByRole("alert")).toHaveTextContent("连接失败");
  expect(screen.getByRole("button", { name: "重试" })).toBeEnabled();
});

it("重检请求成功但后端仍有启动故障时保留错误页", async () => {
  api.read.mockResolvedValue({ ...missing, startupDetectionFailed: true });
  mount();
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(api.refresh).toHaveBeenCalledOnce());
  expect(screen.getByRole("alert")).toHaveTextContent("本次检测未能完整完成");
  expect(screen.queryByText("设置智能体")).not.toBeInTheDocument();
});

it("保存请求成功但后端未解除准入时仍保留引导", async () => {
  api.read.mockResolvedValue(found);
  api.update.mockResolvedValue(undefined);
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "模型与权限配置尚未就绪",
  );
  expect(screen.queryByText("工作区")).not.toBeInTheDocument();
});

it("单页同时列出已安装与未安装 CLI，安装命令使用图标复制并保留官方入口", async () => {
  api.read.mockResolvedValue({
    ...found,
    resources: [...(found.resources ?? []), missing.resources?.[0] ?? {}],
  });
  const user = userEvent.setup();
  mount();
  expect(
    await screen.findByRole("button", { name: "OpenCode" }),
  ).toHaveAttribute("aria-pressed", "true");
  await user.click(screen.getByRole("button", { name: "Codex" }));
  expect(screen.getByText("npm install -g @openai/codex")).toBeVisible();
  const copy = screen.getByRole("button", { name: "复制安装命令" });
  expect(copy.querySelector("svg")).not.toBeNull();
  await user.click(copy);
  expect(await navigator.clipboard.readText()).toBe(
    "npm install -g @openai/codex",
  );
  expect(screen.getByRole("link", { name: "查看官方安装指南" })).toBeVisible();
  expect(api.update).not.toHaveBeenCalled();
});

it("超过十个模型复用搜索选择，按用户所选模型与支持权限保存", async () => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  const models = [
    {
      id: "free-model",
      name: "免费模型",
      permissionLevels: ["auto", "default"],
    },
    ...Array.from({ length: 11 }, (_, i) => ({
      id: `model-${i + 1}`,
      name: `模型 ${i + 1}`,
      permissionLevels: ["default"],
    })),
  ];
  api.read.mockResolvedValue({
    ...found,
    resources: [{ ...found.resources?.[0], models }],
  });
  const user = userEvent.setup();
  mount();
  await user.click(await screen.findByRole("button", { name: "模型" }));
  await user.type(
    screen.getByRole("searchbox", { name: "搜索模型" }),
    "模型 11",
  );
  expect(
    screen.queryByRole("radio", { name: "免费模型" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("radio", { name: "模型 11" }));
  await user.click(
    screen.getByRole("button", { name: "设为默认并进入 NeuxBot" }),
  );
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.objectContaining({
          defaultModel: "model-11",
          defaultPermissionLevel: "default",
        }),
      }),
      expect.anything(),
    ),
  );
  vi.unstubAllGlobals();
});
