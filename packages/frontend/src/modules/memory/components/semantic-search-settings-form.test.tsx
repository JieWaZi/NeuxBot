import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SemanticSearchSettingsForm } from "./semantic-search-settings-form";
import "@/shared/i18n";

const updateSettings = vi.hoisted(() => vi.fn());

const statusState = vi.hoisted(() => ({
  current: {
    configured: false,
    conflict: false,
    needsReindex: false,
  },
}));

vi.mock("../api/memory-queries", () => ({
  getMemorySettingsOptions: () => ({
    queryFn: async () => ({
      apiKeyConfigured: false,
      baseUrl: "",
      model: "",
    }),
    queryKey: ["memory-settings"],
  }),
  getMemoryStatusOptions: () => ({
    queryFn: async () => statusState.current,
    queryKey: ["memory-status"],
  }),
  invalidateMemoryQueries: vi.fn(),
  reindexMemoryIndexMutation: () => ({ mutationFn: vi.fn() }),
  testMemoryEmbeddingMutation: () => ({ mutationFn: vi.fn() }),
  updateMemorySettingsMutation: () => ({ mutationFn: updateSettings }),
}));

function renderForm(onClose = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SemanticSearchSettingsForm onClose={onClose} />
    </QueryClientProvider>,
  );
}

describe("SemanticSearchSettingsForm", () => {
  beforeEach(() => {
    updateSettings.mockReset().mockResolvedValue({});
    statusState.current = {
      configured: false,
      conflict: false,
      needsReindex: false,
    };
  });

  it("introduces semantic search once and exposes the configuration fields", async () => {
    renderForm();

    const description =
      "用于找到说法不同、意思相近的记忆。此设备上的所有伙伴共用这项设置。";
    expect(await screen.findByText(description)).toBeVisible();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      screen.getByRole("form", { name: "记忆语义检索" }),
    ).toHaveAccessibleDescription(description);
    expect(screen.queryByRole("heading", { name: "Embedding" })).toBeNull();
    expect(
      screen.getByPlaceholderText("https://api.openai.com/v1"),
    ).toBeVisible();
    expect(screen.getByPlaceholderText("text-embedding-3-small")).toBeVisible();
    expect(screen.getByLabelText("API Key")).toBeVisible();
    expect(screen.getByRole("button", { name: "测试连接" })).toBeDisabled();
    expect(screen.getByText(/需要重建记忆索引/)).toBeVisible();
  });

  it("keeps the necessary update action inside settings", async () => {
    statusState.current = {
      configured: true,
      conflict: false,
      needsReindex: true,
    };
    renderForm();

    expect(
      await screen.findByRole("button", { name: "更新现有记忆" }),
    ).toBeEnabled();
  });

  it("keeps invalid drafts open and collapses after saving valid settings", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    renderForm(onClose);
    const url = await screen.findByLabelText("服务地址");
    await user.type(url, "ftp://example.test/v1");
    await user.click(screen.getByRole("button", { name: "保存设置" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "服务地址必须是 HTTP 或 HTTPS 地址。",
    );
    expect(updateSettings).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();

    await user.clear(url);
    await user.type(url, "https://example.test/v1");
    await user.type(screen.getByLabelText("模型"), "example-model");
    await user.click(screen.getByRole("button", { name: "保存设置" }));
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(updateSettings.mock.calls[0]?.[0]).toMatchObject({
      body: {
        apiKey: "",
        baseUrl: "https://example.test/v1",
        clearApiKey: false,
        model: "example-model",
      },
    });
  });

  it("collapses an unsubmitted draft without writing settings", async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    renderForm(onClose);
    await user.type(await screen.findByLabelText("模型"), "unsaved");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(updateSettings).not.toHaveBeenCalled();
  });
});
