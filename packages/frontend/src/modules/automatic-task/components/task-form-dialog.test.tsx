import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TaskFormDialog } from "./task-dialogs";
import { ApiError } from "@/shared/api/errors";
import type * as AutomaticTaskSDK from "@/shared/api/generated/sdk.gen";
import "@/shared/i18n";

const { create, update, selectDirectory } = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  selectDirectory: vi.fn(),
}));
vi.mock("@/shared/platform", () => ({ selectDirectory }));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof AutomaticTaskSDK>()),
  createAutomaticTask: create,
  updateAutomaticTask: update,
  listWakers: async () => ({
    data: {
      resources: [
        { id: "waker-1", name: "私人助理" },
        { id: "waker-2", name: "资料助手" },
      ],
      totalSize: 2,
    },
  }),
  listChannels: async () => ({
    data: {
      resources: [
        {
          channel: { id: "team-1", name: "资料团队", leadWakerId: "waker-2" },
          members: [
            {
              waker: { id: "waker-1", name: "私人助理" },
              runtimeId: "codex",
              accountId: "system",
              model: "model-1",
              reasoning: "high",
              permissionLevel: "default",
            },
            {
              waker: { id: "waker-2", name: "资料助手" },
              runtimeId: "codex",
              accountId: "system",
              model: "model-1",
              reasoning: "high",
              permissionLevel: "full-access",
            },
          ],
        },
      ],
      totalSize: 1,
    },
  }),
  listRuntimes: async () => ({
    data: {
      resources: [
        {
          id: "codex",
          name: "Codex",
          baseRuntimeId: "codex",
          enabled: true,
          state: "ready",
          isDefault: true,
          defaultModel: "model-1",
          defaultReasoning: "high",
          models: [
            {
              id: "model-1",
              name: "模型一",
              permissionLevels: ["default", "auto", "full-access"],
              reasoningOptions: [{ id: "high", name: "High" }],
            },
          ],
        },
      ],
    },
  }),
  listAiAccounts: async () => ({ data: { resources: [] } }),
  listProjects: async () => ({ data: { resources: [] } }),
  previewAutomaticSchedule: async () => ({
    data: { nextRuns: [Date.now() + 60000], runsIn24Hours: 1 },
  }),
}));

beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  // jsdom 的 RAF 时间原点与 Vitest 全局 performance 不同，滚轮动画需同一时钟。
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(performance.now()), 16),
  );
  vi.stubGlobal("cancelAnimationFrame", (id: number) =>
    window.clearTimeout(id),
  );
  create.mockReset().mockResolvedValue({ data: { taskId: "task-1" } });
  update.mockReset().mockResolvedValue({ data: { taskId: "task-1" } });
  selectDirectory.mockReset().mockResolvedValue("/workspace/authorized");
});

afterEach(() => vi.unstubAllGlobals());

it("新建 API 任务冻结智能体、模型、账号和授权目录，并提供集成 Key 指引", async () => {
  const user = userEvent.setup();
  const saved = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        wakerId="waker-1"
        onOpenChange={vi.fn()}
        onSaved={saved}
      />
    </QueryClientProvider>,
  );
  const shell = screen.getByRole("dialog", { name: "新建自动任务" });
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  expect(screen.getByRole("dialog", { name: "新建自动任务" })).toBe(shell);
  await user.type(await screen.findByLabelText("任务名称"), "每日归档");
  await user.type(screen.getByLabelText("执行指令"), "整理项目资料");
  await user.click(screen.getByRole("radio", { name: "API" }));
  await user.click(screen.getByRole("radio", { name: "本地目录" }));
  await user.click(screen.getByRole("button", { name: "本地目录" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "本地目录" })).toHaveTextContent(
      "/workspace/authorized",
    ),
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(create.mock.calls[0]?.[0]).toMatchObject({
    path: { wakerId: "waker-1" },
    body: {
      name: "每日归档",
      instruction: "整理项目资料",
      trigger: "api",
      runtimeId: "codex",
      model: "model-1",
      reasoning: "high",
      accountId: "system",
      permissionLevel: "default",
      enabled: true,
      projectId: "",
      workingDirectory: "/workspace/authorized",
    },
  });
  await waitFor(() =>
    expect(saved).toHaveBeenCalledWith({
      taskId: "task-1",
    }),
  );
});

it("编辑保留启停、执行配置和编辑版本，保存失败时保留草稿以便重试", async () => {
  update.mockRejectedValueOnce(
    new ApiError("配置已变化", "AutomaticTaskRevisionConflict", 409),
  );
  const user = userEvent.setup();
  const saved = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        wakerId="waker-1"
        onOpenChange={vi.fn()}
        onSaved={saved}
        task={{
          id: "task-1",
          name: "旧名称",
          instruction: "已有指令",
          trigger: "api",
          revision: 7,
          runtimeId: "codex",
          accountId: "system",
          model: "model-1",
          reasoning: "high",
          permissionLevel: "auto",
          workingDirectory: "/workspace/authorized",
          enabled: false,
        }}
      />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  await user.clear(screen.getByLabelText("任务名称"));
  await user.type(await screen.findByLabelText("任务名称"), "新名称");
  await user.click(screen.getByRole("button", { name: "使用会话临时目录" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("配置已变化");
  expect(screen.getByLabelText("任务名称")).toHaveValue("新名称");
  expect(saved).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(saved).toHaveBeenCalledWith({ taskId: "task-1" }));
  expect(update.mock.calls[1]?.[0]).toMatchObject({
    path: { wakerId: "waker-1", taskId: "task-1" },
    body: {
      revision: 7,
      name: "新名称",
      enabled: false,
      permissionLevel: "auto",
      instruction: "已有指令",
      model: "model-1",
      reasoning: "high",
      workingDirectory: "",
    },
  });
});

it("指令框内选择默认模型、推理和权限，并保存同一份执行配置", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        wakerId="waker-1"
        onOpenChange={vi.fn()}
        onSaved={vi.fn()}
        task={{
          id: "task-1",
          name: "资料整理",
          instruction: "整理项目资料",
          trigger: "api",
          revision: 3,
          runtimeId: "codex",
          accountId: "system",
          model: "model-1",
          reasoning: "high",
          permissionLevel: "default",
          workingDirectory: "/workspace/authorized",
          enabled: false,
        }}
      />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  const model = await screen.findByRole("button", { name: /^模型一/ });
  await user.click(model);
  await user.click(screen.getByRole("button", { name: "选择模型" }));
  await user.click(screen.getByRole("radio", { name: "默认模型" }));
  const slider = screen.getByRole("slider");
  await waitFor(() => expect(slider).toHaveFocus());
  await user.keyboard("{End}");
  expect(slider).toHaveAttribute("aria-valuetext", "High");
  await user.click(screen.getByTestId("composer-permission-control"));
  await user.click(screen.getByRole("menuitemradio", { name: /^完全访问/ }));
  expect(screen.getByLabelText("执行指令")).toHaveValue("整理项目资料");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    body: {
      model: "",
      reasoning: "high",
      permissionLevel: "full-access",
      instruction: "整理项目资料",
    },
  });
});

it("定时表单在一次性与定期之间切换，并将运行时间保存为小时和分钟", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        wakerId="waker-1"
        onOpenChange={vi.fn()}
        onSaved={vi.fn()}
        task={{
          id: "task-1",
          name: "每日归档",
          instruction: "整理项目资料",
          trigger: "schedule",
          revision: 3,
          runtimeId: "codex",
          accountId: "system",
          model: "model-1",
          reasoning: "high",
          permissionLevel: "default",
          workingDirectory: "/workspace/authorized",
          enabled: true,
          schedule: {
            kind: "daily",
            hour: 9,
            minute: 0,
          },
        }}
      />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");

  expect(screen.getByRole("button", { name: "时区" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  expect(screen.getByRole("button", { name: "时区" })).toHaveTextContent(
    "Asia/Shanghai",
  );
  expect(
    screen.queryByRole("combobox", { name: "时区" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("radio", { name: "一次性" }));
  expect(screen.getByRole("button", { name: "日期与时间" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "运行时间" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  await user.click(screen.getByRole("radio", { name: "定期" }));
  expect(screen.getByRole("button", { name: "运行时间" })).toHaveTextContent(
    "09:00",
  );
  await user.click(screen.getByRole("button", { name: "运行时间" }));
  screen.getByRole("spinbutton", { name: "小时" }).focus();
  await user.keyboard("13");
  await waitFor(
    () =>
      expect(screen.getByRole("spinbutton", { name: "小时" })).toHaveAttribute(
        "aria-valuenow",
        "13",
      ),
    { timeout: 5000 },
  );
  screen.getByRole("spinbutton", { name: "分钟" }).focus();
  await user.keyboard("45");
  await waitFor(
    () =>
      expect(screen.getByRole("spinbutton", { name: "分钟" })).toHaveAttribute(
        "aria-valuenow",
        "45",
      ),
    { timeout: 5000 },
  );
  await user.click(screen.getByRole("button", { name: "完成" }));
  expect(screen.getByRole("button", { name: "运行时间" })).toHaveTextContent(
    "13:45",
  );
  await user.click(screen.getByRole("button", { name: "时区" }));
  expect(screen.getByRole("combobox", { name: "时区" })).toHaveAttribute(
    "aria-required",
    "false",
  );
  await user.click(screen.getByRole("combobox", { name: "时区" }));
  await user.type(
    screen.getByRole("searchbox", { name: "搜索时区…" }),
    "tokyo",
  );
  await user.keyboard("{ArrowDown}{Enter}");
  expect(screen.getByRole("combobox", { name: "时区" })).toHaveTextContent(
    "Asia/Tokyo",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    body: {
      revision: 3,
      trigger: "schedule",
      schedule: {
        kind: "daily",
        hour: 13,
        minute: 45,
        timezone: "Asia/Tokyo",
      },
    },
  });
});

it("全局新建选择团队；输入框默认继承成员设置且不创建覆盖", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog open onOpenChange={vi.fn()} onSaved={vi.fn()} />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  await user.type(await screen.findByLabelText("任务名称"), "团队归档");
  await user.click(screen.getByRole("radio", { name: "团队" }));
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "执行者" })).toBeEnabled(),
  );
  screen.getByRole("combobox", { name: "执行者" }).focus();
  await user.keyboard("{ArrowDown}");
  await screen.findByRole("option", { name: /资料团队/ });
  await user.keyboard("{Enter}");
  expect(
    await screen.findByText(
      "默认沿用团队设置；自定义成员配置只用于这个自动任务。",
    ),
  ).toBeVisible();
  expect(
    screen.queryByRole("combobox", { name: "智能体" }),
  ).not.toBeInTheDocument();
  expect(screen.getByTestId("composer-runtime-control")).toBeVisible();
  expect(screen.getByRole("button", { name: "配置成员" })).toHaveTextContent(
    "资料助手",
  );
  expect(screen.getByText("完全访问")).toBeVisible();
  await user.type(screen.getByLabelText("执行指令"), "整理团队资料");
  await user.click(screen.getByRole("radio", { name: "API" }));
  await user.click(screen.getByRole("radio", { name: "本地目录" }));
  await user.click(screen.getByRole("button", { name: "本地目录" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(create.mock.calls[0]?.[0]).toMatchObject({
    path: { wakerId: "waker-2" },
    body: {
      channelId: "team-1",
      memberConfigs: [],
      executorWakerId: "",
      runtimeId: "",
      model: "",
      reasoning: "",
      accountId: "system",
      permissionLevel: "default",
      workingDirectory: "/workspace/authorized",
    },
  });
});

it("编辑团队任务可切换成员自定义配置并分别恢复团队设置", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        onOpenChange={vi.fn()}
        onSaved={vi.fn()}
        task={{
          id: "task-1",
          wakerId: "waker-2",
          channelId: "team-1",
          name: "团队归档",
          instruction: "整理资料",
          trigger: "api",
          enabled: false,
          revision: 4,
          workingDirectory: "/workspace/authorized",
          memberConfigs: [
            {
              wakerId: "waker-1",
              runtimeId: "codex",
              accountId: "system",
              model: "",
              reasoning: "",
              permissionLevel: "auto",
            },
          ],
        }}
      />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  await waitFor(
    () => expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
    { timeout: 5000 },
  );
  // 默认显示负责人，编辑另一位成员不会改变负责人配置。
  await user.click(screen.getByRole("button", { name: "配置成员" }));
  await user.click(
    await screen.findByRole("menuitemradio", { name: /私人助理/ }),
  );
  expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
    "默认模型",
  );
  expect(screen.getByRole("button", { name: /自动审批/ })).toBeVisible();
  await user.click(screen.getByTestId("composer-model-control"));
  await user.click(screen.getByRole("button", { name: "选择模型" }));
  await user.click(await screen.findByRole("radio", { name: "模型一" }));
  await user.click(screen.getByRole("button", { name: "配置成员" }));
  await user.click(
    await screen.findByRole("menuitemradio", { name: /资料助手/ }),
  );
  expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
    "模型一",
  );
  expect(screen.getByRole("button", { name: /完全访问/ })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "配置成员" }));
  await user.click(
    await screen.findByRole("menuitemradio", { name: /私人助理/ }),
  );
  expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
    "模型一",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    body: {
      memberConfigs: [
        {
          wakerId: "waker-1",
          runtimeId: "codex",
          accountId: "system",
          model: "model-1",
          reasoning: "",
          permissionLevel: "auto",
        },
      ],
    },
  });
  // 保存回调不关闭此测试弹窗，继续验证恢复继承后清空覆盖。
  await user.click(screen.getByRole("button", { name: "配置成员" }));
  await user.click(
    await screen.findByRole("menuitem", { name: "沿用团队设置" }),
  );
  expect(screen.getByRole("button", { name: /默认权限/ })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledTimes(2));
  expect(update.mock.calls[1]?.[0]).toMatchObject({
    body: { memberConfigs: [] },
  });
});

it.each(["project", "directory"])(
  "%s 未选择工作范围时允许保存并交给会话临时目录",
  async (scope) => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <TaskFormDialog
          open
          wakerId="waker-1"
          onOpenChange={vi.fn()}
          onSaved={vi.fn()}
        />
      </QueryClientProvider>,
    );
    await vi.dynamicImportSettled();
    await screen.findByLabelText("任务名称");
    await user.type(await screen.findByLabelText("任务名称"), "临时目录任务");
    await user.type(screen.getByLabelText("执行指令"), "整理会话资料");
    await user.click(screen.getByRole("radio", { name: "API" }));
    if (scope === "directory") {
      await user.click(screen.getByRole("radio", { name: "本地目录" }));
    }
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
    );
    expect(
      screen.getByLabelText(scope === "project" ? "伙伴项目" : "本地目录"),
    ).not.toHaveAttribute("aria-required", "true");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    const body = create.mock.calls[0]?.[0].body;
    expect(body.projectId || "").toBe("");
    expect(body.workingDirectory || "").toBe("");
  },
);
