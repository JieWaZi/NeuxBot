import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import "@/shared/i18n";
import { AutomaticTaskAPISettingsDialog } from "./task-dialogs";
import { apiExamples } from "./task-api-examples";

const { getAPI, setAPI } = vi.hoisted(() => ({
  getAPI: vi.fn(),
  setAPI: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  getApiService: getAPI,
  updateApiService: setAPI,
}));

beforeEach(() => {
  getAPI.mockReset().mockResolvedValue({
    data: {
      ip: "127.0.0.1",
      port: 8431,
      available: true,
      enabled: false,
      mcpEnabled: true,
      publicBaseUrl: "https://example.test/api/v1",
    },
  });
  setAPI.mockReset().mockResolvedValue({ data: {} });
});

function renderSettings() {
  function Harness() {
    const [open, setOpen] = useState(true);
    return (
      <>
        <button onClick={() => setOpen(true)}>重新打开</button>
        <AutomaticTaskAPISettingsDialog open={open} onOpenChange={setOpen} />
      </>
    );
  }
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Harness />
    </QueryClientProvider>,
  );
}

it("确认才提交 IP 和端口，刷新服务器设置并关闭弹窗", async () => {
  const user = userEvent.setup();
  renderSettings();
  const shell = screen.getByRole("dialog", { name: "API 服务设置" });
  await vi.dynamicImportSettled();
  const ip = await screen.findByLabelText(/IP 地址/);
  expect(screen.getByRole("dialog", { name: "API 服务设置" })).toBe(shell);
  await waitFor(() => expect(ip).toHaveValue("127.0.0.1"));
  await user.clear(ip);
  await user.type(ip, "192.168.1.20");
  const port = screen.getByLabelText(/端口/);
  await user.clear(port);
  await user.type(port, "18431");
  expect(setAPI).not.toHaveBeenCalled();
  getAPI.mockResolvedValue({
    data: {
      ip: "192.168.1.20",
      port: 18431,
      available: true,
      enabled: false,
      publicBaseUrl: "https://example.test/api/v1",
    },
  });
  await user.click(screen.getByRole("button", { name: "确认" }));
  await waitFor(() => expect(setAPI).toHaveBeenCalledOnce());
  expect(setAPI.mock.calls[0]?.[0]).toMatchObject({
    body: {
      ip: "192.168.1.20",
      port: 18431,
      enabled: false,
      publicBaseUrl: "https://example.test/api/v1",
    },
  });
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  await user.click(screen.getByRole("button", { name: "重新打开" }));
  await waitFor(() =>
    expect(screen.getByLabelText(/IP 地址/)).toHaveValue("192.168.1.20"),
  );
  expect(screen.getByLabelText(/端口/)).toHaveValue(18431);
});

it("取消不提交，重新打开恢复已保存设置；端口越界不能确认", async () => {
  const user = userEvent.setup();
  renderSettings();
  await waitFor(() =>
    expect(screen.getByLabelText(/IP 地址/)).toHaveValue("127.0.0.1"),
  );
  await user.clear(screen.getByLabelText(/IP 地址/));
  await user.type(screen.getByLabelText(/IP 地址/), "::1");
  await user.clear(screen.getByLabelText(/端口/));
  await user.type(screen.getByLabelText(/端口/), "65536");
  expect(screen.getByRole("button", { name: "确认" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(setAPI).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "重新打开" }));
  await waitFor(() =>
    expect(screen.getByLabelText(/IP 地址/)).toHaveValue("127.0.0.1"),
  );
  expect(screen.getByLabelText(/端口/)).toHaveValue(8431);
});

it("确认失败保留草稿和弹窗，允许修正后重试", async () => {
  const user = userEvent.setup();
  renderSettings();
  await waitFor(() =>
    expect(screen.getByLabelText(/IP 地址/)).toHaveValue("127.0.0.1"),
  );
  await user.clear(screen.getByLabelText(/端口/));
  await user.type(screen.getByLabelText(/端口/), "18431");
  setAPI.mockRejectedValueOnce(new Error("occupied"));
  await user.click(screen.getByRole("button", { name: "确认" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(screen.getByLabelText(/端口/)).toHaveValue(18431);
  await user.click(screen.getByRole("button", { name: "确认" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(setAPI).toHaveBeenCalledTimes(2);
});

it("调用示例保留反代前缀并使用应用级 API Key", () => {
  const example = apiExamples("task", "https://example.test/proxy/api/v1");
  expect(example.endpoint).toBe(
    "https://example.test/proxy/api/v1/automatic-tasks/task/runs",
  );
  expect(example.trigger).toContain("X-API-Key: <API_KEY>");
  expect(example.status).toContain("X-API-Key: <API_KEY>");
  expect(example.trigger).not.toContain("Bearer");
});

it("只有 MCP 开启时，共享监听可用也不表示 API 已启动", async () => {
  renderSettings();
  expect(
    await screen.findByText("API 不可用，请检查 IP 和端口。"),
  ).toBeVisible();
  expect(screen.queryByText("API 已启动")).not.toBeInTheDocument();
});

it("API 已启用且真实监听可用时显示 API 已启动", async () => {
  getAPI.mockResolvedValue({
    data: {
      ip: "127.0.0.1",
      port: 8431,
      available: true,
      enabled: true,
      mcpEnabled: false,
    },
  });
  renderSettings();
  expect(await screen.findByText("API 已启动")).toBeVisible();
});
