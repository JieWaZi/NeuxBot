import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import type {
  GlobalEventFrame,
  GlobalEventObserver,
} from "@/shared/api/global-events";
import type { GetRuntimeInstallationResponse as Snapshot } from "@/shared/api/generated/types.gen";
import {
  RuntimeInstallationControls,
  runtimeInstallationOptions,
  type RuntimeInstallationControlsProps,
} from "./runtime-installation-controls";
import "@/shared/i18n";

const { read, install, cancel, check, observers, recoveries, scene, current } =
  vi.hoisted(() => ({
    read: vi.fn(),
    install: vi.fn(),
    cancel: vi.fn(),
    check: vi.fn(),
    observers: new Set<GlobalEventObserver>(),
    recoveries: new Set<() => void>(),
    scene: { active: true },
    current: { snapshot: {} as Snapshot },
  }));
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  getRuntimeInstallation: read,
  installRuntime: install,
  cancelRuntimeInstallation: cancel,
  checkRuntimeUpdates: check,
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
  useRouteSceneActive: () => scene.active,
}));

const clients: QueryClient[] = [];
beforeEach(() => {
  scene.active = true;
  current.snapshot = {
    runtimeId: "codex",
    state: "idle",
    supported: true,
    canInstall: true,
    canUpdate: true,
    canUninstall: false,
    installed: false,
    cancelable: false,
    revision: 0,
  };
  read.mockReset().mockImplementation(async () => ({ data: current.snapshot }));
  install.mockReset().mockResolvedValue({ data: {} });
  cancel.mockReset().mockResolvedValue({ data: {} });
  check.mockReset().mockImplementation(async () => {
    current.snapshot = {
      ...current.snapshot,
      checkState: "checked",
      latestVersion: "1.0.0",
      updateAvailable: false,
      revision: (current.snapshot.revision ?? 0) + 1,
    };
    return { data: {} };
  });
});
afterEach(() => {
  clients.forEach((client) => client.clear());
  clients.length = 0;
  observers.clear();
  recoveries.clear();
});

function show(props: Partial<RuntimeInstallationControlsProps> = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  const input = { runtimeId: "codex", installed: false, ...props };
  const content = () => (
    <QueryClientProvider client={client}>
      <RuntimeInstallationControls {...input} />
    </QueryClientProvider>
  );
  const view = render(content());
  return { client, ...view, refresh: () => view.rerender(content()) };
}

function changed(runtimeId = "codex") {
  const frame: GlobalEventFrame = {
    protocolVersion: 1,
    epoch: "core-1",
    connectionId: "connection-1",
    deliverySequence: 1,
    topic: "runtimes",
    type: "runtimes.event",
    resource: { kind: "runtime", id: runtimeId },
    eventId: "installation-1",
    payload: { type: "runtime.installation.changed" },
  };
  for (const observer of observers) observer.onEvent(frame);
}

it("首次读取显示等待；点击安装发送真实生成mutation并读取后台进度", async () => {
  let resolveRead: ((value: { data: Snapshot }) => void) | undefined;
  read.mockImplementationOnce(
    () => new Promise((resolve) => (resolveRead = resolve)),
  );
  show();
  expect(
    screen.getByRole("status", { name: "正在读取安装状态" }),
  ).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  await act(async () => resolveRead?.({ data: current.snapshot }));
  install.mockImplementation(async () => {
    current.snapshot = {
      ...current.snapshot,
      state: "installing",
      phase: "downloading",
      revision: 1,
      cancelable: true,
      progress: 37,
      totalBytes: 100,
      receivedBytes: 37,
    };
    return { data: {} };
  });
  await userEvent.click(await screen.findByRole("button", { name: "安装" }));
  expect(install).toHaveBeenCalledWith(
    expect.objectContaining({ path: { runtimeId: "codex" } }),
  );
  expect(await screen.findByText("正在下载 37%")).toBeInTheDocument();
  expect(screen.getByRole("progressbar", { name: "安装进度" })).toHaveAttribute(
    "aria-valuenow",
    "37",
  );
  expect(screen.getByRole("button", { name: "取消安装" })).toBeInTheDocument();
});

it("已安装但尚未检查时只检查版本，确认最新后不显示更新动作", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "installed",
    installed: true,
    version: "1.0.0",
  };
  show({ installed: true, version: "1.0.0" });
  const button = await screen.findByRole("button", { name: "检查更新" });
  expect(install).not.toHaveBeenCalled();
  expect(button).toHaveClass("border-resource-border");
  await userEvent.click(button);
  await waitFor(() => expect(check).toHaveBeenCalledTimes(1));
  expect(await screen.findByText("当前无需更新")).toBeVisible();
  expect(install).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "更新" }),
  ).not.toBeInTheDocument();
});

it("仅官方检查确认存在更高版本时提供更新并发起安装", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "installed",
    installed: true,
    version: "1.0.0",
    latestVersion: "1.1.0",
    checkState: "checked",
    updateAvailable: true,
  };
  show({ installed: true });
  await userEvent.click(await screen.findByRole("button", { name: "更新" }));
  await waitFor(() => expect(install).toHaveBeenCalledTimes(1));
  expect(check).not.toHaveBeenCalled();
});

it("受管程序损坏时同版本也直接重新安装，不误判无需更新", async () => {
  current.snapshot = {
    ...current.snapshot,
    installed: false,
    canInstall: false,
    canUpdate: true,
    state: "failed",
    maintenanceReason: "repairable",
    version: "1.0.0",
    latestVersion: "1.0.0",
    checkState: "checked",
    updateAvailable: false,
    errorCode: "RuntimeInstallationFailed",
  };
  show({ installed: true });
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "程序文件已损坏或丢失，请重新安装。",
  );
  expect(screen.queryByText("当前无需更新")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "重新安装" }));
  await waitFor(() => expect(install).toHaveBeenCalledTimes(1));
  expect(check).not.toHaveBeenCalled();
});

it("安装失败仍保留官方下载入口且只打开对应官网", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  current.snapshot = {
    ...current.snapshot,
    state: "failed",
    errorCode: "RuntimeInstallationFailed",
  };
  show({ runtimeId: "grok", baseRuntimeId: "grok" });
  const official = await screen.findByRole("button", { name: "官方下载" });
  expect(official.textContent).toBe("");
  expect(official).not.toHaveClass("border");
  expect(official.querySelector("svg")).toHaveClass("lucide-external-link");
  await userEvent.hover(official);
  expect(await screen.findByRole("tooltip")).toHaveTextContent("官方下载");
  await userEvent.click(official);
  expect(open).toHaveBeenCalledWith(
    "https://grok.com/build",
    "_blank",
    "noreferrer",
  );
  expect(install).not.toHaveBeenCalled();
  open.mockRestore();
});

it("官方服务504显示安全且可理解的原因，不再一律展示安装失败", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "failed",
    errorCode: "RuntimeInstallationFailed",
    errorReason: "http_status",
    errorStatus: 504,
  };
  show();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "官方服务返回 HTTP 504",
  );
  expect(screen.getByRole("button", { name: "官方下载" })).toBeVisible();
});

it("程序已丢失时权威快照覆盖父列表旧安装状态，提供真正重试安装", async () => {
  current.snapshot = {
    ...current.snapshot,
    installed: false,
    state: "failed",
    errorCode: "RuntimeInstallationFailed",
  };
  show({ installed: true });
  await userEvent.click(await screen.findByRole("button", { name: "重试" }));
  await waitFor(() => expect(install).toHaveBeenCalledTimes(1));
  expect(check).not.toHaveBeenCalled();
});

it("删除后台任务使用删除文案和取消删除，不冒充下载", async () => {
  current.snapshot = {
    ...current.snapshot,
    operation: "uninstall",
    state: "installing",
    phase: "uninstalling",
    cancelable: true,
    installed: true,
  };
  show({ installed: true });
  expect(await screen.findByText("正在删除")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "取消删除" }));
  await waitFor(() => expect(cancel).toHaveBeenCalledOnce());
  expect(install).not.toHaveBeenCalled();
});

it("当前安装归属不支持自动更新时不会拿另一份下载冒充更新", async () => {
  current.snapshot = {
    ...current.snapshot,
    installed: true,
    canUpdate: false,
    updateAvailable: true,
    checkState: "checked",
    latestVersion: "2.0.0",
  };
  show({ installed: true });
  expect(await screen.findByRole("button", { name: "检查更新" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "更新" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "官方下载" })).toBeVisible();
  expect(install).not.toHaveBeenCalled();
});

it("重开详情恢复任务，取消成功仍由GET终态通知父级一次", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "installing",
    phase: "verifying",
    revision: 4,
    cancelable: true,
  };
  const onRuntimeChanged = vi.fn(async () => undefined);
  const view = show({ onRuntimeChanged });
  expect(await screen.findByText("正在验证")).toBeInTheDocument();
  expect(screen.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
  expect(onRuntimeChanged).not.toHaveBeenCalled();
  cancel.mockImplementation(async () => {
    current.snapshot = {
      ...current.snapshot,
      state: "cancelled",
      revision: 5,
      cancelable: false,
      errorCode: "OperationCanceled",
    };
    return { data: {} };
  });
  await userEvent.click(screen.getByRole("button", { name: "取消安装" }));
  expect(cancel).toHaveBeenCalledWith(
    expect.objectContaining({ path: { runtimeId: "codex" } }),
  );
  expect(await screen.findByText("安装已取消")).toBeInTheDocument();
  await waitFor(() => expect(onRuntimeChanged).toHaveBeenCalledTimes(1));
  view.refresh();
  expect(onRuntimeChanged).toHaveBeenCalledTimes(1);
  expect(screen.queryByText("OperationCanceled")).toBeNull();
});

it("后台不可取消时不显示取消，未知内部阶段使用通用中文文案", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "installing",
    phase: "private-new-phase",
    revision: 2,
    cancelable: false,
  };
  show();
  expect(await screen.findByText("正在安装")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "取消安装" })).toBeNull();
  expect(screen.queryByText("private-new-phase")).toBeNull();
});

it("安装失败显示领域文案并可重试，命令失败不泄露后端代码", async () => {
  current.snapshot = {
    ...current.snapshot,
    state: "failed",
    revision: 3,
    errorCode: "RuntimeInstallationFailed",
  };
  install.mockRejectedValueOnce(new Error("raw-internal-secret"));
  show();
  expect(await screen.findByRole("alert")).toHaveTextContent(/失败.*重试/);
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByText("安装操作失败，请重试")).toBeInTheDocument();
  expect(
    screen.queryByText(/RuntimeInstallationFailed|raw-internal-secret/),
  ).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(install).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("安装操作失败，请重试")).toBeNull();
});

it("首次GET失败提供读取重试；刷新失败保留已有安装操作", async () => {
  read.mockRejectedValueOnce(new Error("private-get-error"));
  const view = show();
  expect(await screen.findByText("无法读取安装状态")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(
    await screen.findByRole("button", { name: "安装" }),
  ).toBeInTheDocument();
  read.mockRejectedValueOnce(new Error("refresh-failed"));
  await act(async () => {
    await view.client.invalidateQueries({
      queryKey: runtimeInstallationOptions("codex").queryKey,
    });
  });
  expect(await screen.findByText("无法读取安装状态")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "安装" })).toBeInTheDocument();
  expect(screen.queryByText(/private-get-error|refresh-failed/)).toBeNull();
});

it("平台不支持时只显示明确状态，无空安装按钮", async () => {
  current.snapshot = {
    ...current.snapshot,
    supported: false,
    canInstall: false,
    canUpdate: false,
  };
  show();
  expect(
    await screen.findByText("当前平台暂不支持受管安装"),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  expect(screen.getByRole("button", { name: "官方下载" })).toBeVisible();
});

it("事件仅刷新当前runtime或基础runtime；隐藏场景不读，恢复对账且无轮询", async () => {
  const view = show({ runtimeId: "work-profile", baseRuntimeId: "codex" });
  await screen.findByRole("button", { name: "安装" });
  act(() => changed("claude"));
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(read).toHaveBeenCalledTimes(1);
  act(() => changed("codex"));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  scene.active = false;
  view.refresh();
  act(() => changed("work-profile"));
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(read).toHaveBeenCalledTimes(2);
  scene.active = true;
  view.refresh();
  await waitFor(() => expect(read).toHaveBeenCalledTimes(3));
  await new Promise((resolve) => setTimeout(resolve, 1000));
  expect(read).toHaveBeenCalledTimes(3);
  view.unmount();
  expect(observers.size).toBe(0);
  expect(recoveries.size).toBe(0);
});

it("迟到低修订GET不能覆盖较新快照；Core恢复接受新的revision零", async () => {
  let resolveRead: ((value: { data: Snapshot }) => void) | undefined;
  read.mockImplementationOnce(
    () => new Promise((resolve) => (resolveRead = resolve)),
  );
  const view = show();
  act(() => {
    view.client.setQueryData(runtimeInstallationOptions("codex").queryKey, {
      ...current.snapshot,
      state: "installing",
      phase: "downloading",
      revision: 8,
      cancelable: true,
      progress: 70,
      totalBytes: 100,
    });
  });
  expect(await screen.findByText("正在下载 70%")).toBeInTheDocument();
  await act(async () =>
    resolveRead?.({
      data: { ...current.snapshot, state: "idle", revision: 1 },
    }),
  );
  expect(screen.getByText("正在下载 70%")).toBeInTheDocument();
  act(() => {
    for (const recover of recoveries) recover();
  });
  expect(
    await screen.findByRole("button", { name: "安装" }),
  ).toBeInTheDocument();
  expect(
    view.client.getQueryData<Snapshot>(
      runtimeInstallationOptions("codex").queryKey,
    )?.revision,
  ).toBe(0);
});
