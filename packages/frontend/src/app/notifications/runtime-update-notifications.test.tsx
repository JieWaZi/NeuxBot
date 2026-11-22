import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";

import "@/app/workspace-i18n";
import { Toaster, toast } from "@/shared/ui/sonner";
import { RuntimeUpdateNotifications } from "./runtime-update-notifications";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: vi.fn() }));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
}));
vi.mock("@/shared/api/runtime-queries", () => ({
  listRuntimesOptions: () => ({ queryKey: ["runtimes"], staleTime: Infinity }),
  runtimeInstallationOptions: (id: string) => ({
    queryKey: ["installation", id],
    staleTime: Infinity,
  }),
}));

afterEach(() => act(() => toast.dismiss()));

function Location() {
  const { pathname, search } = useLocation();
  return <output>{pathname + search}</output>;
}

it("仅读取已安装 CLI 的可用更新，关闭后同版本不重弹，新版本仍可查看", async () => {
  const client = new QueryClient();
  client.setQueryData(["runtimes"], {
    resources: [
      {
        id: "codex",
        baseRuntimeId: "codex",
        name: "Codex CLI",
        installed: true,
      },
      {
        id: "codex-other",
        baseRuntimeId: "codex",
        name: "Other profile",
        installed: true,
      },
      {
        id: "cursor",
        baseRuntimeId: "cursor",
        name: "Cursor",
        installed: false,
      },
    ],
  });
  const receipt = {
    checkState: "checked",
    updateAvailable: true,
    version: "v1.0.0",
    latestVersion: "v2.0.0",
  };
  client.setQueryData(["installation", "codex"], receipt);
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <RuntimeUpdateNotifications />
        <Toaster />
        <Location />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("v1.0.0 → v2.0.0")).toBeVisible();
  expect(client.getQueryState(["installation", "codex-other"])).toBeUndefined();
  expect(client.getQueryState(["installation", "cursor"])).toBeUndefined();
  fireEvent.click(
    screen.getByRole("button", { name: "关闭通知「Codex CLI 有可用更新」" }),
  );
  act(() =>
    client.setQueryData(["installation", "codex"], { ...receipt, revision: 2 }),
  );
  await waitFor(() =>
    expect(screen.queryByText("Codex CLI 有可用更新")).not.toBeInTheDocument(),
  );
  act(() =>
    client.setQueryData(["installation", "codex"], {
      ...receipt,
      latestVersion: "3.0.0",
      revision: 3,
    }),
  );
  expect(await screen.findByText("v1.0.0 → v3.0.0")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "查看更新" }));
  expect(screen.getByText("/settings/agents?agent=codex")).toBeVisible();
  await waitFor(() =>
    expect(screen.queryByText("Codex CLI 有可用更新")).not.toBeInTheDocument(),
  );
  client.clear();
});
