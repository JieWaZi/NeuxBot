import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import type {
  GlobalEventFrame,
  GlobalEventObserver,
} from "@/shared/api/global-events";
import type { GetRuntimeAuthenticationResponse as Snapshot } from "@/shared/api/generated/types.gen";
import {
  RuntimeSystemAccountActions,
  runtimeAuthenticationOptions,
} from "./runtime-authentication-controls";
import "@/shared/i18n";

const { read, start, cancel, observers, recoveries, current } = vi.hoisted(
  () => ({
    read: vi.fn(),
    start: vi.fn(),
    cancel: vi.fn(),
    observers: new Set<GlobalEventObserver>(),
    recoveries: new Set<() => void>(),
    current: { snapshot: {} as Snapshot },
  }),
);
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getRuntimeAuthentication: read,
  authenticateRuntime: start,
  cancelRuntimeAuthentication: cancel,
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    observers.add(observer);
    return () => observers.delete(observer);
  },
  registerCoreConnectionRecovery: (callback: () => void) => {
    recoveries.add(callback);
    return () => recoveries.delete(callback);
  },
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
const clients: QueryClient[] = [];
beforeEach(() => {
  current.snapshot = {
    scope: "system",
    state: "idle",
    methods: [{ id: "grok.com", name: "Grok", kind: "agent" }],
    canAuthenticate: true,
    apiKeySupported: true,
    terminalRequired: false,
    cancelable: false,
    revision: 1,
  };
  read.mockReset().mockImplementation(async () => ({ data: current.snapshot }));
  start.mockReset().mockImplementation(async () => {
    current.snapshot = {
      ...current.snapshot,
      state: "authenticating",
      jobId: "login-1",
      revision: 2,
      cancelable: true,
    };
    return { data: current.snapshot };
  });
  cancel.mockReset().mockResolvedValue({ data: {} });
});
afterEach(() => {
  clients.forEach((client) => client.clear());
  clients.length = 0;
  observers.clear();
  recoveries.clear();
});
function show(auth = "missing", runtimeId = "grok") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <RuntimeSystemAccountActions
        runtime={{
          id: runtimeId,
          baseRuntimeId: runtimeId,
          installed: true,
          auth,
        }}
      />
    </QueryClientProvider>,
  );
  return client;
}
function changed(runtimeId = "grok") {
  const frame: GlobalEventFrame = {
    protocolVersion: 1,
    epoch: "core-1",
    connectionId: "connection-1",
    deliverySequence: 1,
    topic: "runtimes",
    type: "runtimes.event",
    resource: { kind: "runtime", id: runtimeId },
    eventId: "auth-1",
    payload: { type: "runtime.authentication.changed" },
  };
  observers.forEach((observer) => observer.onEvent(frame));
}

it("需要登录时直接启动系统原生认证，完成后由事件读取安全快照", async () => {
  show();
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  await waitFor(() =>
    expect(start).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "grok" },
        body: { methodId: "grok.com" },
      }),
    ),
  );
  expect(
    await screen.findByText("如果弹出浏览器页面，请在那里完成登录。"),
  ).toBeVisible();
  current.snapshot = {
    ...current.snapshot,
    state: "succeeded",
    result: "authentication_completed",
    revision: 3,
    cancelable: false,
  };
  act(changed);
  expect(await screen.findByText("登录完成")).toBeVisible();
});
it("终端或环境变量方法不伪造浏览器登录，也不重复密钥分组入口", async () => {
  current.snapshot = {
    ...current.snapshot,
    canAuthenticate: false,
    methods: [],
    terminalRequired: true,
  };
  show("missing");
  await waitFor(() => expect(read).toHaveBeenCalledOnce());
  expect(
    screen.queryByRole("button", { name: "配置 API 密钥" }),
  ).not.toBeInTheDocument();
  expect(start).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "登录" }),
  ).not.toBeInTheDocument();
});
it("已认证账号显示重新连接，取消只携带本弹层的精确任务身份", async () => {
  show("ok");
  await userEvent.click(
    await screen.findByRole("button", { name: "重新连接" }),
  );
  await screen.findByText("如果弹出浏览器页面，请在那里完成登录。");
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "grok" },
        query: { jobId: "login-1" },
      }),
    ),
  );
});
it.each(["checking", "authenticating"] as const)(
  "关闭正在发起的登录后，%s 回执返回时仍取消同一任务",
  async (state) => {
    let resolveStart: ((value: { data: Snapshot }) => void) | undefined;
    start.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveStart = resolve;
        }),
    );
    show();
    await userEvent.click(await screen.findByRole("button", { name: "登录" }));
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    await act(async () =>
      resolveStart?.({
        data: {
          ...current.snapshot,
          state,
          jobId: "late-1",
          cancelable: true,
          revision: 2,
        },
      }),
    );
    await waitFor(() =>
      expect(cancel).toHaveBeenCalledWith(
        expect.objectContaining({ query: { jobId: "late-1" } }),
      ),
    );
  },
);

it("真实checking回执显示准备状态且关闭时取消，不再提供重复启动按钮", async () => {
  start.mockImplementationOnce(async () => ({
    data: {
      ...current.snapshot,
      state: "checking",
      jobId: "preparing-1",
      cancelable: true,
      revision: 2,
    },
  }));
  show();
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  expect(await screen.findByText("正在准备登录")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({ query: { jobId: "preparing-1" } }),
    ),
  );
});
it("原生任务已被别处替换时，旧弹层不取消新任务", async () => {
  show();
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  await screen.findByText("如果弹出浏览器页面，请在那里完成登录。");
  current.snapshot = {
    ...current.snapshot,
    jobId: "other-window",
    revision: 3,
  };
  act(changed);
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(cancel).not.toHaveBeenCalled();
});

it("Core重连后，旧Core迟到的登录回执不能覆盖新快照或恢复旧弹层", async () => {
  let resolveStart: ((value: { data: Snapshot }) => void) | undefined;
  start.mockImplementation(
    () =>
      new Promise((resolve) => {
        resolveStart = resolve;
      }),
  );
  const client = show();
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  current.snapshot = { ...current.snapshot, state: "idle", revision: 0 };
  act(() => recoveries.forEach((recover) => recover()));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  await act(async () =>
    resolveStart?.({
      data: {
        ...current.snapshot,
        jobId: "old-core-job",
        state: "checking",
        cancelable: true,
        revision: 999,
      },
    }),
  );
  expect(
    client.getQueryData(runtimeAuthenticationOptions("grok").queryKey),
  ).toMatchObject({ state: "idle", revision: 0 });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(cancel).not.toHaveBeenCalled();
});

it("能力重试使用显式refresh，旧Core迟到的重试不覆盖重连后的低修订", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "failed",
    jobId: "",
    errorCode: "RuntimeUnavailable",
    canAuthenticate: false,
  };
  let resolveRefresh: ((value: { data: Snapshot }) => void) | undefined;
  read.mockImplementation(async (options) => {
    if (options.query?.refresh)
      return new Promise((resolve) => {
        resolveRefresh = resolve;
      });
    return { data: current.snapshot };
  });
  const client = show();
  await userEvent.click(await screen.findByRole("button", { name: "重试" }));
  await waitFor(() => expect(resolveRefresh).toBeDefined());
  current.snapshot = {
    ...current.snapshot,
    state: "idle",
    errorCode: undefined,
    revision: 0,
  };
  act(() => recoveries.forEach((recover) => recover()));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(3));
  await act(async () =>
    resolveRefresh?.({
      data: { ...current.snapshot, state: "checking", revision: 999 },
    }),
  );
  expect(
    client.getQueryData(runtimeAuthenticationOptions("grok").queryKey),
  ).toMatchObject({ state: "idle", revision: 0 });
});

it.each(["existing_login_verified", "connection_verified"] as const)(
  "Codex %s 只确认连接，不声称打开浏览器或完成新授权",
  async (result) => {
    start.mockResolvedValueOnce({
      data: {
        ...current.snapshot,
        jobId: "verified-1",
        state: "succeeded",
        result,
        revision: 2,
      },
    });
    show("ok", "codex");
    await userEvent.click(
      await screen.findByRole("button", { name: "验证连接" }),
    );
    expect(await screen.findByText("已确认当前连接")).toBeVisible();
    expect(screen.queryByText("登录完成")).not.toBeInTheDocument();
    expect(
      screen.queryByText("请在浏览器中完成登录。"),
    ).not.toBeInTheDocument();
    if (result === "existing_login_verified")
      expect(screen.getByText("继续使用本机现有账号。")).toBeVisible();
  },
);

it("再次连接等待POST时不闪回上一次成功结果", async () => {
  start.mockResolvedValueOnce({
    data: {
      ...current.snapshot,
      jobId: "first",
      state: "succeeded",
      result: "authentication_completed",
      revision: 2,
    },
  });
  let resolveStart: ((value: { data: Snapshot }) => void) | undefined;
  show();
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  await screen.findByText("登录完成");
  await userEvent.click(screen.getByRole("button", { name: "确定" }));
  start.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveStart = resolve;
      }),
  );
  await userEvent.click(screen.getByRole("button", { name: "登录" }));
  expect(await screen.findByText("正在准备登录")).toBeVisible();
  expect(screen.queryByText("登录完成")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await act(async () =>
    resolveStart?.({
      data: {
        ...current.snapshot,
        state: "checking",
        jobId: "second",
        cancelable: true,
        revision: 3,
      },
    }),
  );
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({ query: { jobId: "second" } }),
    ),
  );
});

it("验证现有Codex连接期间不提示等待浏览器授权", async () => {
  let resolveStart: ((value: { data: Snapshot }) => void) | undefined;
  start.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveStart = resolve;
      }),
  );
  show("ok", "codex");
  await userEvent.click(
    await screen.findByRole("button", { name: "验证连接" }),
  );
  expect(await screen.findByText("正在验证连接…")).toBeVisible();
  expect(screen.getByText("正在确认本机账号的连接状态。")).toBeVisible();
  expect(
    screen.queryByText("如果弹出浏览器页面，请在那里完成登录。"),
  ).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await act(async () =>
    resolveStart?.({
      data: {
        ...current.snapshot,
        jobId: "verified-after-close",
        state: "succeeded",
        result: "existing_login_verified",
        revision: 2,
      },
    }),
  );
  expect(cancel).not.toHaveBeenCalled();
});

it("支持密钥的 Kimi 仍展示内置账号的本机配置入口", async () => {
  current.snapshot = {
    ...current.snapshot,
    methods: [],
    canAuthenticate: false,
    apiKeySupported: true,
    configurationReason: "terminal_authentication",
  };
  show("missing", "kimi");
  expect(
    await screen.findByRole("button", { name: "官方配置说明" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "登录" }),
  ).not.toBeInTheDocument();
});

it("Kimi 内置账号用登录入口展示真实设备码并按任务取消", async () => {
  current.snapshot = {
    ...current.snapshot,
    methods: [{ id: "login", name: "Kimi", kind: "agent" }],
  };
  show("missing", "kimi");
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  expect(
    screen.queryByRole("button", { name: "官方配置说明" }),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(start).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "kimi" },
        body: { methodId: "login" },
      }),
    ),
  );
  current.snapshot = {
    ...current.snapshot,
    verificationUrl:
      "https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH",
    userCode: "ABCD-EFGH",
    revision: 3,
  };
  act(() => changed("kimi"));
  expect(await screen.findByText("ABCD-EFGH")).toBeVisible();
  expect(screen.getByRole("button", { name: "打开官方授权页" })).toBeVisible();
  expect(screen.queryByText("登录完成")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "kimi" },
        query: { jobId: "login-1" },
      }),
    ),
  );
});

it("Kimi 原生授权完成后显示成功结果并收起设备码", async () => {
  current.snapshot = {
    ...current.snapshot,
    methods: [{ id: "login", name: "Kimi", kind: "agent" }],
  };
  show("missing", "kimi");
  await userEvent.click(await screen.findByRole("button", { name: "登录" }));
  await waitFor(() => expect(start).toHaveBeenCalledOnce());
  current.snapshot = {
    ...current.snapshot,
    state: "succeeded",
    result: "authentication_completed",
    cancelable: false,
    revision: 3,
  };
  act(() => changed("kimi"));
  expect(await screen.findByText("登录完成")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "打开官方授权页" }),
  ).not.toBeInTheDocument();
});
