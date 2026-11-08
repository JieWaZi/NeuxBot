import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { AccountLogin } from "./account-login";
import "@/shared/i18n";
const {
  start,
  poll,
  cancel,
  openAuthorization,
  login,
  stream,
  refreshRuntime,
} = vi.hoisted(() => ({
  start: vi.fn(),
  refreshRuntime: vi.fn(async () => ({})),
  stream: {
    observer: undefined as GlobalEventObserver | undefined,
    stops: 0,
  },
  poll: vi.fn(),
  cancel: vi.fn(),
  openAuthorization: vi.fn(),
  login: {
    id: "job-1",
    userCode: "CODE",
    verificationUrl: "https://auth.openai.com/codex/device",
    intervalMs: 10,
    complete: false,
    expiresAt: Date.now() + 60_000,
  },
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  startAiAccountLoginMutation: () => ({ mutationFn: start }),
  cancelAiAccountLoginMutation: () => ({ mutationFn: cancel }),
  listAiAccountsQueryKey: () => ["accounts"],
  listRuntimesQueryKey: () => ["runtimes"],
}));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getAiAccountLogin: poll,
  refreshRuntime,
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    stream.observer = observer;
    return () => {
      stream.stops++;
      if (stream.observer === observer) stream.observer = undefined;
    };
  },
}));
vi.mock("@/shared/platform", () => ({ openExternalURL: openAuthorization }));
beforeEach(() => {
  start.mockReset().mockResolvedValue(login);
  poll.mockReset().mockResolvedValue({ data: login });
  stream.observer = undefined;
  stream.stops = 0;
  cancel.mockReset().mockResolvedValue({});
  openAuthorization.mockReset().mockResolvedValue(undefined);
});
it("快照网络失败后保留授权码，手动重试同一授权并完成登录", async () => {
  poll
    .mockRejectedValueOnce(new Error("网络暂时不可用"))
    .mockResolvedValue({ data: { ...login, complete: true } });
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <AccountLogin accountId="account-1" onClose={onClose} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  await waitFor(() =>
    expect(openAuthorization).toHaveBeenCalledWith(login.verificationUrl),
  );
  await user.click(screen.getByRole("button", { name: "打开官方授权页" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("网络暂时不可用");
  expect(screen.getByText("CODE")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByText("登录成功，账号已保存。")).toBeVisible();
  expect(start).toHaveBeenCalledTimes(1);
  expect(openAuthorization).toHaveBeenCalledTimes(2);
  await user.click(screen.getByRole("button", { name: "确定" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(cancel).not.toHaveBeenCalled();
  expect(poll).toHaveBeenLastCalledWith(
    expect.objectContaining({
      path: { accountId: "account-1", loginId: "job-1" },
    }),
  );
});
it("关闭窗口取消正在读取的快照及服务端授权任务", async () => {
  let signal: AbortSignal | undefined;
  poll.mockImplementation((options: { signal: AbortSignal }) => {
    signal = options.signal;
    return new Promise(() => {});
  });
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={onClose} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  await waitFor(() => expect(poll).toHaveBeenCalled());
  await user.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() => expect(signal?.aborted).toBe(true));
  await act(async () => {
    await new Promise((resolve) => window.setTimeout(resolve, 30));
  });
  expect(poll).toHaveBeenCalledTimes(1);
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { accountId: "account-1", loginId: "job-1" },
      }),
      expect.anything(),
    ),
  );
  expect(onClose).toHaveBeenCalled();
});
it("自动打开授权页失败时展示可复制的官方地址", async () => {
  openAuthorization.mockRejectedValue(new Error("无法打开浏览器"));
  start.mockResolvedValue({ ...login, intervalMs: 8000 });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={vi.fn()} />
    </QueryClientProvider>,
  );

  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "未能自动打开官方授权页",
  );
  expect(screen.getByText(login.verificationUrl)).toBeVisible();
  expect(screen.getByRole("button", { name: "打开官方授权页" })).toBeVisible();
});

it("授权进度通过统一事件完成，空闲等待没有周期快照请求", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  await waitFor(() => expect(stream.observer).toBeDefined());
  await waitFor(() => expect(poll).toHaveBeenCalledTimes(1));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 80));
  });
  expect(poll).toHaveBeenCalledTimes(1);
  act(() =>
    stream.observer?.onEvent({
      protocolVersion: 1,
      epoch: "test",
      connectionId: "test",
      deliverySequence: 1,
      eventId: "completed",
      topic: "account-login",
      type: "account-login.updated",
      resource: { kind: "account", id: "account-1", parentId: login.id },
      payload: { login: { ...login, complete: true } },
    }),
  );
  expect(await screen.findByText("登录成功，账号已保存。")).toBeVisible();
  expect(poll).toHaveBeenCalledTimes(1);
});

it("晚返回的等待快照不能覆盖已推送的成功，重连只重新对账一次", async () => {
  let resolve: ((value: { data: typeof login }) => void) | undefined;
  poll.mockImplementationOnce(
    () =>
      new Promise<{ data: typeof login }>((done) => {
        resolve = done;
      }),
  );
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  await waitFor(() => expect(resolve).toBeDefined());
  act(() =>
    stream.observer?.onEvent({
      protocolVersion: 1,
      epoch: "test",
      connectionId: "test",
      deliverySequence: 1,
      eventId: "completed",
      topic: "account-login",
      type: "account-login.updated",
      resource: { kind: "account", id: "account-1", parentId: login.id },
      payload: { login: { ...login, complete: true } },
    }),
  );
  await act(async () => {
    resolve?.({ data: login });
  });
  expect(await screen.findByText("登录成功，账号已保存。")).toBeVisible();
  act(() => stream.observer?.onResync?.("connection-restored"));
  await waitFor(() => expect(poll).toHaveBeenCalledTimes(2));
  expect(screen.getByText("登录成功，账号已保存。")).toBeVisible();
});

it("默认账号登录完成后重新探测所属智能体", async () => {
  refreshRuntime.mockClear();
  poll.mockResolvedValue({ data: { ...login, complete: true } });
  const user = userEvent.setup();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AccountLogin
        accountId="default-account"
        runtimeId="codex"
        onClose={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  await waitFor(() =>
    expect(refreshRuntime).toHaveBeenCalledWith(
      expect.objectContaining({ path: { runtimeId: "codex" } }),
    ),
  );
  expect(await screen.findByText("登录成功，账号已保存。")).toBeVisible();
});

it("Claude 浏览器授权不要求设备码或验证链接，保留等待与取消入口", async () => {
  const browserLogin = {
    ...login,
    userCode: "",
    verificationUrl: "",
    complete: false,
  };
  start.mockResolvedValue(browserLogin);
  poll.mockResolvedValue({ data: browserLogin });
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <AccountLogin
        accountId="claude-subscription"
        browserAuthorization
        onClose={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "登录" }));
  expect(
    await screen.findByText("如果弹出浏览器页面，请在那里完成登录。"),
  ).toBeVisible();
  expect(screen.getByText("等待授权完成…")).toBeVisible();
  expect(screen.getByRole("button", { name: "取消" })).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(openAuthorization).not.toHaveBeenCalled();
});

it("关闭尚未返回的受管登录后仍取消精确的新作业，不再打开链接", async () => {
  let resolveStart: ((value: typeof login) => void) | undefined;
  start.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveStart = resolve;
      }),
  );
  const user = userEvent.setup();
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={onClose} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  expect(await screen.findByText("正在准备登录")).toBeVisible();
  await user.keyboard("{Escape}");
  await act(async () => resolveStart?.({ ...login, id: "late-managed" }));
  await waitFor(() =>
    expect(cancel).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { accountId: "account-1", loginId: "late-managed" },
      }),
      expect.anything(),
    ),
  );
  expect(openAuthorization).not.toHaveBeenCalled();
  expect(onClose).toHaveBeenCalledOnce();
});

it("设备码复制能力不可用时保留可手动选择的授权码并就地提示", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AccountLogin accountId="account-1" onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  await user.click(screen.getByRole("button", { name: "获取授权码" }));
  const descriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: undefined,
  });
  try {
    await user.click(await screen.findByRole("button", { name: "复制授权码" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "无法复制，请手动选择授权码。",
    );
    expect(screen.getByLabelText("设备授权码")).toHaveTextContent("CODE");
  } finally {
    if (descriptor) Object.defineProperty(navigator, "clipboard", descriptor);
    else Reflect.deleteProperty(navigator, "clipboard");
  }
});
