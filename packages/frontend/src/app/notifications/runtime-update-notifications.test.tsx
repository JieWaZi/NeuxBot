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
vi.mock("@/shared/api/runtime-queries", async () => {
  const { skipToken } = await import("@tanstack/react-query");
  return {
    listRuntimesOptions: () => ({ queryKey: ["runtimes"], queryFn: skipToken }),
    runtimeInstallationOptions: (id: string) => ({
      queryKey: ["installation", id],
      queryFn: skipToken,
    }),
  };
});

afterEach(() => {
  act(() => toast.dismiss());
  window.localStorage.clear();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function Location() {
  const { pathname, search } = useLocation();
  return <output>{pathname + search}</output>;
}

it("仅读取已安装 CLI 的可用更新，关闭后当天不重弹，次日仍可查看", async () => {
  vi.setSystemTime(new Date(2026, 10, 25, 12));
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
  vi.setSystemTime(new Date(2026, 10, 26, 12));
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

it("同一天重开时不再展示已提示的 CLI，次日同版本仍可提示", () => {
  const offer = vi.spyOn(toast, "manual");
  const resources = [
    { id: "codex", baseRuntimeId: "codex", name: "Codex CLI", installed: true },
    { id: "cursor", baseRuntimeId: "cursor", name: "Cursor", installed: true },
  ];
  const receipt = {
    checkState: "checked",
    updateAvailable: true,
    version: "v1.0.0",
    latestVersion: "v2.0.0",
  };
  function open(latestVersion: string) {
    const client = new QueryClient();
    client.setQueryData(["runtimes"], { resources });
    client.setQueryData(["installation", "codex"], {
      ...receipt,
      latestVersion,
    });
    client.setQueryData(["installation", "cursor"], receipt);
    const view = render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <RuntimeUpdateNotifications />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    view.unmount();
    client.clear();
    act(() => toast.dismiss());
  }

  vi.setSystemTime(new Date(2026, 10, 25, 23, 58));
  open("v2.0.0");
  expect(offer.mock.calls.map(([title]) => title)).toEqual([
    "Codex CLI 有可用更新",
    "Cursor 有可用更新",
  ]);

  vi.setSystemTime(new Date(2026, 10, 25, 23, 59));
  open("v2.0.0");
  open("v3.0.0");
  expect(offer).toHaveBeenCalledTimes(2);

  vi.setSystemTime(new Date(2026, 10, 26, 0, 0));
  open("v2.0.0");
  expect(offer).toHaveBeenCalledTimes(4);
});
