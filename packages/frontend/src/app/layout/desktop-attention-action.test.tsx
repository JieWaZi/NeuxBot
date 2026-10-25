import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { getNotificationSnapshotQueryKey } from "@/modules/notification";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ApplicationUpdateState } from "@/shared/platform/application-update";
import { DesktopAttentionAction } from "./desktop-attention-action";
import "@/shared/i18n";

const updates = vi.hoisted(() => ({
  state: undefined as ApplicationUpdateState | undefined,
  busy: false,
  download: vi.fn(),
  openInstaller: vi.fn(),
}));
vi.mock("@/shared/platform/application-update", () => ({
  useApplicationUpdate: () => updates,
}));
const clients = new Set<QueryClient>();

function setup() {
  const onOpen = vi.fn();
  const onOpenUpdates = vi.fn();
  const client = new QueryClient();
  clients.add(client);
  client.setQueryData(getNotificationSnapshotQueryKey(), {
    epoch: "a".repeat(32),
    revision: 1,
    synced: true,
    actionCount: 2,
    unreadResultCount: 0,
    unreadFailureCount: 0,
    totalCount: 2,
    actionPreview: [],
    unreadPreview: [],
    generatedAt: 100,
  });
  function Navigation() {
    const navigate = useNavigate();
    return (
      <DesktopAttentionAction
        active={false}
        connected
        onOpen={() => {
          onOpen();
          navigate("/work-board");
        }}
        onOpenUpdates={onOpenUpdates}
      />
    );
  }
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter>
          <Routes>
            <Route path="/" element={<Navigation />} />
            <Route path="/work-board" element={<h1>任务看板</h1>} />
          </Routes>
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { onOpen, onOpenUpdates, user: userEvent.setup() };
}

beforeEach(() => {
  updates.state = undefined;
  updates.busy = false;
  vi.clearAllMocks();
});

afterEach(() => {
  for (const client of clients) client.clear();
  clients.clear();
});

describe("软件更新占用铃铛入口", () => {
  it("没有可用更新时保持原通知入口", async () => {
    const { onOpen, user } = setup();
    await user.click(
      screen.getByRole("button", { name: "通知 2 项，打开任务看板" }),
    );
    expect(onOpen).toHaveBeenCalledOnce();
  });
  for (const status of ["available", "downloading", "downloaded"] as const) {
    it(`${status} 状态定位关于软件，并按状态下载或打开安装包`, async () => {
      updates.state = {
        revision: 1,
        currentVersion: "0.1.0",
        platform: "darwin",
        arch: "arm64",
        status,
        downloadedBytes: 50,
        release: {
          version: "0.2.0",
          url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
          notes: "更新",
          publishedAt: "2026-10-16T00:00:00Z",
          downloadSize: 100,
        },
      };
      const { onOpenUpdates, user } = setup();
      await user.click(
        screen.getByRole("button", {
          name:
            status === "available"
              ? "下载新版本 v0.2.0"
              : status === "downloaded"
                ? "打开 v0.2.0 安装包"
                : "新版本下载中 50%，查看进度",
        }),
      );
      expect(onOpenUpdates).toHaveBeenCalledOnce();
      expect(updates.download).toHaveBeenCalledTimes(
        status === "available" ? 1 : 0,
      );
      expect(updates.openInstaller).toHaveBeenCalledTimes(
        status === "downloaded" ? 1 : 0,
      );
    });
    it(`${status} 时尚未打开看板标签也能从通知入口导航到工作看板`, async () => {
      updates.state = {
        revision: 1,
        currentVersion: "0.1.0",
        platform: "darwin",
        arch: "arm64",
        status,
        downloadedBytes: 50,
        release: {
          version: "0.2.0",
          url: "https://github.com/JieWaZi/NeuxBot/releases/tag/v0.2.0",
          notes: "更新",
          publishedAt: "2026-10-16T00:00:00Z",
          downloadSize: 100,
        },
      };
      const { onOpenUpdates, user } = setup();
      await user.click(
        screen.getByRole("button", { name: "通知 2 项，打开任务看板" }),
      );
      expect(
        screen.getByRole("heading", { name: "任务看板" }),
      ).toBeInTheDocument();
      expect(onOpenUpdates).not.toHaveBeenCalled();
      expect(updates.download).not.toHaveBeenCalled();
      expect(updates.openInstaller).not.toHaveBeenCalled();
    });
  }
});
