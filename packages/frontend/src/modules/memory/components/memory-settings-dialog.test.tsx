import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MemorySettingsDialog } from "./memory-settings-dialog";
import "@/shared/i18n";

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
  updateMemorySettingsMutation: () => ({ mutationFn: vi.fn() }),
}));

function renderDialog() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemorySettingsDialog open onOpenChange={vi.fn()} />
    </QueryClientProvider>,
  );
}

describe("MemorySettingsDialog", () => {
  it("loads the memory-owned embedding settings", async () => {
    statusState.current = {
      configured: false,
      conflict: false,
      needsReindex: false,
    };
    renderDialog();

    expect(await screen.findByText("Embedding")).toBeVisible();
    expect(
      screen.getByPlaceholderText("https://api.openai.com/v1"),
    ).toBeVisible();
    expect(screen.getByPlaceholderText("text-embedding-3-small")).toBeVisible();
  });

  it("keeps the necessary update action inside settings", async () => {
    statusState.current = {
      configured: true,
      conflict: false,
      needsReindex: true,
    };
    renderDialog();

    expect(await screen.findByText("Embedding")).toBeVisible();
    expect(screen.getByRole("button", { name: "更新现有记忆" })).toBeEnabled();
  });
});
