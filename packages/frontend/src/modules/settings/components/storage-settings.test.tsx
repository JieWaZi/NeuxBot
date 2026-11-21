import type { GlobalEventObserver } from "@/shared/api/global-events";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  configure,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { platform, configurePlatform } from "@/shared/platform/provider";
import { client } from "@/shared/api/generated/client.gen";
import {
  SceneChromeHost,
  SceneChromePortalProvider,
} from "@/shared/ui/scene-chrome-portal";
import { SettingsScene } from "../pages/settings-scene";
import { settingsSectionOf } from "../settings-navigation";

const events = vi.hoisted(() => ({
  observers: new Set<GlobalEventObserver>(),
}));
vi.mock(import("@/shared/api/global-events"), async (load) => ({
  ...(await load()),
  subscribeGlobalEvents: (_topic, _resource, observer) => {
    events.observers.add(observer);
    return () => {
      events.observers.delete(observer);
    };
  },
}));

const root = "http://127.0.0.1:8080/api/v1/storage";
const policy = { enabled: false, retentionDays: 15, revision: 3, valid: true };
const usage = {
  totalBytes: 1024,
  scannedAt: 1700000000000,
  complete: false,
  dataRoot: "/data",
  managedRoot: "/work",
  categories: [
    { category: "work_files", bytes: 1024, paths: ["/work/Wakers"] },
  ],
  disks: [{ path: "/work", freeBytes: 2048, totalBytes: 4096 }],
};
const server = setupServer(
  http.get("http://127.0.0.1:8080/api/v1/wakers/:wakerId", () =>
    HttpResponse.json({}),
  ),
  http.get(root + "/usage", () => HttpResponse.json(usage)),
  http.get(root + "/entities", () =>
    HttpResponse.json({ items: [], total: 0 }),
  ),
  http.get(root + "/policy", () => HttpResponse.json(policy)),
  http.get(root + "/maintenance", () => HttpResponse.json({})),
  http.post(root + "/expired-chats/preview", () =>
    HttpResponse.json({
      id: "preview-1",
      total: 1,
      complete: true,
      knownBytes: 1024,
    }),
  ),
  http.post(root + "/cleanup-preview", () =>
    HttpResponse.json({
      id: "resource-1",
      items: [{ category: "logs", available: true, bytes: 1024 }],
    }),
  ),
);
beforeAll(() => {
  configure({ asyncUtilTimeout: 5000 });
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function TestLocation() {
  const location = useLocation();
  return (
    <output aria-label="当前地址">
      {location.pathname}
      {location.search}
    </output>
  );
}

function RoutedStorageSettingsScene() {
  const [params] = useSearchParams();
  return (
    <SettingsScene
      section={settingsSectionOf("storage")}
      storageOwnerId={
        params.get("files") === "wakers"
          ? params.get("owner") || undefined
          : undefined
      }
      onClose={() => {}}
    />
  );
}

function openSettings(href = "/settings/storage") {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[href]}>
        <TestLocation />
        <SceneChromePortalProvider>
          <header aria-label="设置顶栏">
            <SceneChromeHost />
          </header>
          <RoutedStorageSettingsScene />
        </SceneChromePortalProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return user;
}

it("存储总览合并底层分类，只显示磁盘剩余容量", async () => {
  server.use(
    http.get(root + "/usage", () =>
      HttpResponse.json({
        ...usage,
        totalBytes: 8270,
        categories: [
          { category: "work_files", bytes: 1102 },
          { category: "conversations", bytes: 1024 },
          { category: "shared_native", bytes: 1024 },
          { category: "knowledge", bytes: 1024 },
          { category: "logs", bytes: 1024 },
          { category: "cache", bytes: 2048 },
          { category: "other", bytes: 1024 },
        ],
      }),
    ),
  );
  openSettings();
  const total = await screen.findByText("至少 8 KB");
  expect(total).toBeVisible();
  expect(total.closest('[data-slot="settings-card"]')).toBeNull();
  expect(screen.getByText("磁盘剩余 2 KB")).toBeVisible();
  expect(screen.getByText("会话").parentElement).toHaveTextContent("2 KB");
  expect(screen.getByText("应用与资源").parentElement).toHaveTextContent(
    "4 KB",
  );
  expect(screen.getByText("知识与记忆")).toBeVisible();
  expect(screen.getByText("工作文件")).toBeVisible();
  expect(screen.queryByText(/\/(work|data)/)).toBeNull();
  expect(screen.queryByText("会话私有文件")).toBeNull();
  expect(screen.queryByText("桌面缓存")).toBeNull();
});

it("仅在确认启用后取得策略令牌并写入原版本", async () => {
  const previews: unknown[] = [],
    writes: unknown[] = [];
  server.use(
    http.post(root + "/expired-chats/preview", async ({ request }) => {
      previews.push(await request.json());
      return HttpResponse.json({
        id: "policy-1",
        total: 0,
        knownBytes: 0,
        complete: true,
      });
    }),
    http.put(root + "/policy", async ({ request }) => {
      writes.push(await request.json());
      return HttpResponse.json({});
    }),
  );
  const user = openSettings();
  const toggle = await screen.findByRole("switch", { name: "自动清理会话" });
  expect(toggle).not.toBeChecked();
  await user.click(toggle);
  let dialog = await screen.findByRole("dialog");
  expect(
    await within(dialog).findByText(/超过 15 天无活动的会话将永久删除/),
  ).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(previews).toEqual([]);
  expect(writes).toEqual([]);
  await user.click(toggle);
  dialog = await screen.findByRole("dialog");
  await user.click(
    await within(dialog).findByRole("button", { name: "确认启用" }),
  );
  await waitFor(() =>
    expect(writes).toEqual([
      {
        enabled: true,
        retentionDays: 15,
        expectedRevision: 3,
        previewId: "policy-1",
      },
    ]),
  );
  expect(previews).toEqual([{ retentionDays: 15 }]);
});

it("会话保留只提供自动清理和天数设置", async () => {
  openSettings();
  await screen.findByRole("switch", { name: "自动清理会话" });
  expect(screen.getByRole("combobox", { name: "保留天数" })).toHaveTextContent(
    "15 天",
  );
  expect(screen.queryByRole("button", { name: "预览待清理会话" })).toBeNull();
  expect(screen.queryByRole("button", { name: "删除所选会话" })).toBeNull();
});

it("清理区合并同类项目，显示可释放大小并隐藏空项", async () => {
  const original = platform();
  configurePlatform({
    ...original,
    kind: "desktop",
    host: {
      ...original.host,
      preview_desktop_storage: async ({ category }) => ({
        available: true,
        bytes: category === "desktop-cache" ? 2048 : 0,
      }),
      clear_desktop_storage: async () => ({ removedBytes: 0 }),
    },
  });
  try {
    server.use(
      http.post(root + "/cleanup-preview", () =>
        HttpResponse.json({
          items: [
            { category: "caches", available: true, bytes: 1024 },
            { category: "logs", available: false, bytes: 8192 },
            { category: "runtimes", available: true, bytes: 0 },
          ],
        }),
      ),
    );
    const user = openSettings();
    expect(await screen.findByText("预计可释放 3 KB")).toBeVisible();
    const button = screen.getByRole("button", { name: "清理缓存" });
    expect(button.closest("section")).toHaveTextContent("3 KB");
    expect(button.querySelector("svg.lucide-trash-2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "一键清理" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "诊断日志" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "未使用的程序" })).toBeNull();
    await user.click(button);
    expect(
      screen.getByRole("dialog", { name: "清理所选资源？" }),
    ).toHaveTextContent("将清理以下项目中可回收的文件：缓存。");
  } finally {
    configurePlatform(original);
  }
});

it("仅确认后清理失败的类别", async () => {
  const reviews: unknown[] = [],
    writes: unknown[] = [];
  server.use(
    http.get(root + "/maintenance", () =>
      HttpResponse.json({
        last: {
          id: "old",
          kind: "resources",
          deleted: 1,
          skipped: 0,
          failed: 1,
          pending: 0,
          removedBytes: 5,
          items: [
            { category: "caches", failed: 0 },
            { category: "logs", failed: 1 },
          ],
        },
      }),
    ),
    http.post(root + "/cleanup-preview", async ({ request }) => {
      reviews.push(await request.json());
      return HttpResponse.json({
        id: "new-resource",
        items: [{ category: "logs", bytes: 1024, available: true }],
      });
    }),
    http.post(root + "/cleanup", async ({ request }) => {
      writes.push(await request.json());
      return HttpResponse.json({ runId: "r2" });
    }),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: "重试失败类别" }));
  const dialog = await screen.findByRole("dialog");
  expect(dialog).toHaveTextContent("诊断日志");
  expect(reviews).not.toContainEqual({ categories: ["logs"] });
  expect(writes).toEqual([]);
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  await waitFor(() => expect(writes).toEqual([{ previewId: "new-resource" }]));
  expect(reviews).toContainEqual({ categories: ["logs"] });
});

it("桌面日志失败后仅重试该类别，页面不显示成功回执", async () => {
  const calls: string[] = [];
  const original = platform();
  configurePlatform({
    ...original,
    kind: "desktop",
    host: {
      ...original.host,
      preview_desktop_storage: async ({ category }) => ({
        available: true,
        bytes: category === "desktop-cache" ? 512 : 256,
      }),
      clear_desktop_storage: async ({ category }) => {
        calls.push(category);
        if (category === "desktop-logs" && calls.length === 2)
          throw Error("busy");
        return { removedBytes: 128 };
      },
    },
  });
  server.use(
    http.post(root + "/cleanup-preview", () =>
      HttpResponse.json({ items: [], id: "" }),
    ),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: "一键清理" }));
  let dialog = await screen.findByRole("dialog", { name: "清理所选资源？" });
  expect(dialog).toHaveTextContent("缓存");
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  const retry = await screen.findByRole("button", { name: "重试失败类别" });
  expect(screen.queryByText(/已移除文件/)).toBeNull();
  await user.click(retry);
  dialog = await screen.findByRole("dialog");
  expect(dialog).toHaveTextContent("诊断日志");
  expect(dialog).not.toHaveTextContent("缓存");
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  await waitFor(() =>
    expect(calls).toEqual(["desktop-cache", "desktop-logs", "desktop-logs"]),
  );
});

it("Core 清理复查失败时，桌面缓存仍可清理，零字节复查结束旧失败", async () => {
  let coreBytes = 1024;
  let corePreviews = 0;
  const desktopClear = vi.fn(async () => ({ removedBytes: 512 }));
  const original = platform();
  configurePlatform({
    ...original,
    kind: "desktop",
    host: {
      ...original.host,
      preview_desktop_storage: async ({ category }) => ({
        available: true,
        bytes: category === "desktop-cache" ? 512 : 0,
      }),
      clear_desktop_storage: desktopClear,
    },
  });
  server.use(
    http.post(root + "/cleanup-preview", () => {
      if (++corePreviews === 2)
        return HttpResponse.json({ message: "failed" }, { status: 500 });
      return HttpResponse.json({
        id: "preview",
        items: [{ category: "caches", bytes: coreBytes, available: true }],
      });
    }),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: "一键清理" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "确认清理",
    }),
  );
  const retry = await screen.findByRole("button", { name: "重试失败类别" });
  expect(desktopClear).toHaveBeenCalledExactlyOnceWith({
    category: "desktop-cache",
  });
  coreBytes = 0;
  await user.click(retry);
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "确认清理",
    }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.queryByRole("button", { name: "重试失败类别" })).toBeNull();
  expect(desktopClear).toHaveBeenCalledTimes(1);
});

it("Core 维护进行中仍能确认桌面缓存清理", async () => {
  const desktopClear = vi.fn(async () => ({ removedBytes: 512 }));
  const original = platform();
  configurePlatform({
    ...original,
    kind: "desktop",
    host: {
      ...original.host,
      preview_desktop_storage: async ({ category }) => ({
        available: true,
        bytes: category === "desktop-cache" ? 512 : 0,
      }),
      clear_desktop_storage: desktopClear,
    },
  });
  server.use(
    http.get(root + "/maintenance", () =>
      HttpResponse.json({ current: { id: "other", startedAt: 1700000000000 } }),
    ),
  );
  const user = openSettings();
  await screen.findByText(/正在清理 ·/);
  expect(screen.getByRole("button", { name: "清理诊断日志" })).toBeDisabled();
  const desktop = await screen.findByRole("button", { name: "清理缓存" });
  expect(desktop).toBeEnabled();
  await user.click(desktop);
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "确认清理",
    }),
  );
  await waitFor(() =>
    expect(desktopClear).toHaveBeenCalledExactlyOnceWith({
      category: "desktop-cache",
    }),
  );
});

it("总览只显示占用最大的十个会话，点行直接打开会话", async () => {
  const requests: string[] = [];
  server.use(
    http.get(root + "/entities", ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("kind") !== "chats")
        return HttpResponse.json({ items: [], total: 0 });
      requests.push(params.get("limit") + ":" + (params.get("cursor") ?? ""));
      return HttpResponse.json({
        items: Array.from(
          { length: Number(params.get("limit")) },
          (_, index) => ({
            id: "chat-" + (index + 1),
            title: "Chat " + (index + 1),
            wakerId: "lumi",
            bytes: 100 - index,
            fileCount: 2,
          }),
        ),
        total: 12,
        nextCursor: "chat-tail",
      });
    }),
  );
  const user = openSettings();
  const tenth = await screen.findByText("Chat 10");
  const chatRow = tenth.closest("button");
  expect(chatRow).toHaveTextContent("2 个附件");
  expect(chatRow?.querySelector("svg.lucide-chevron-right")).toBeNull();
  expect(screen.queryByRole("button", { name: "查看更多" })).toBeNull();
  expect(screen.queryByText("Chat 11")).toBeNull();
  expect(requests).toEqual(["10:"]);
  await user.click(tenth);
  expect(screen.getByLabelText("当前地址")).toHaveTextContent(
    "/chats/chat-10?wakerId=lumi",
  );
});

it("强制统计失败保留旧数，重试恢复", async () => {
  let attempts = 0;
  server.use(
    http.get(root + "/usage", ({ request }) => {
      if (
        new URL(request.url).searchParams.get("force") === "true" &&
        ++attempts === 1
      )
        return new HttpResponse(null, { status: 503 });
      return HttpResponse.json(usage);
    }),
  );
  const user = openSettings();
  expect(await screen.findByText(/至少 1 KB/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重新统计" }));
  expect(await screen.findByText(/显示上次结果/)).toBeVisible();
  expect(screen.getByText(/至少 1 KB/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(screen.queryByText(/显示上次结果/)).toBeNull());
  expect(attempts).toBe(2);
});

it("已启用时缩短保留期确认新天数，取消后保持已保存期限", async () => {
  const previews: unknown[] = [];
  server.use(
    http.get(root + "/policy", () =>
      HttpResponse.json({
        ...policy,
        enabled: true,
        retentionDays: 60,
        nextCheckAt: 1700000000000,
      }),
    ),
    http.post(root + "/expired-chats/preview", async ({ request }) => {
      previews.push(await request.json());
      return HttpResponse.json({
        id: "short-1",
        total: 2,
        complete: false,
        knownBytes: 100,
      });
    }),
  );
  const user = openSettings();
  await screen.findByText(/下次检查/);
  await user.click(screen.getByRole("combobox", { name: "保留天数" }));
  await user.click(screen.getByRole("option", { name: "30 天" }));
  const dialog = await screen.findByRole("dialog");
  expect(await within(dialog).findByText(/超过 30 天/)).toBeVisible();
  expect(dialog).not.toHaveTextContent("100 B");
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(screen.getByRole("combobox", { name: "保留天数" })).toHaveTextContent(
    "60 天",
  );
  expect(previews).toEqual([]);
});

it("键盘取消策略确认后焦点回到原开关", async () => {
  const user = openSettings();
  const toggle = await screen.findByRole("switch", { name: "自动清理会话" });
  toggle.focus();
  await user.keyboard(" ");
  await screen.findByRole("dialog");
  await user.keyboard("{Escape}");
  await waitFor(() => expect(toggle).toHaveFocus());
});

it("Core 离线时保留快照，禁用维护写入，关闭设置时释放订阅", async () => {
  openSettings();
  const toggle = await screen.findByRole("switch", { name: "自动清理会话" });
  act(() => {
    for (const observer of events.observers)
      observer.onState?.("reconnecting", false);
  });
  expect(await screen.findByText(/Core 离线/)).toBeVisible();
  expect(screen.getByText(/至少 1 KB/)).toBeVisible();
  expect(toggle).toBeDisabled();
  cleanup();
  expect(events.observers.size).toBe(0);
});

it("资源清理已接纳但维护状态尚未读回时，保留该运行的待确认提示", async () => {
  let accepted = false;
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(root + "/maintenance", async () => {
      if (accepted) await gate;
      return HttpResponse.json({});
    }),
    http.post(root + "/cleanup", () => {
      accepted = true;
      return HttpResponse.json({ runId: "just-accepted" });
    }),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: "一键清理" }));
  const dialog = await screen.findByRole("dialog", { name: "清理所选资源？" });
  expect(dialog).toHaveTextContent("诊断日志");
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  expect(
    await screen.findByText("请求已接纳，正在读取维护状态。"),
  ).toBeVisible();
  release();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "一键清理" })).toBeEnabled(),
  );
  expect(await screen.findByText(/清理结果未能确认/)).toBeVisible();
  expect(screen.getByRole("button", { name: "重试失败类别" })).toBeEnabled();
});

it("伙伴排行直接打开可搜索的文件列表，删除须确认", async () => {
  let removed = false;
  server.use(
    http.get(root + "/entities", ({ request }) =>
      HttpResponse.json({
        items:
          new URL(request.url).searchParams.get("kind") === "wakers"
            ? [{ id: "lumi", title: "lumi", bytes: 1024, fileCount: 1 }]
            : [],
        total: 1,
      }),
    ),
    http.get(root + "/files", ({ request }) => {
      const params = new URL(request.url).searchParams;
      expect(params.get("kind")).toBe("wakers");
      expect(params.get("id")).toBe("lumi");
      return HttpResponse.json({
        title: "lumi",
        total: removed ? 0 : 1,
        totalBytes: removed ? 0 : 1024,
        complete: true,
        items: removed
          ? []
          : [
              {
                id: "Agents/lumi/notes.md",
                relativePath: "notes.md",
                bytes: 1024,
                source: "workspace",
                version: "generation",
              },
            ],
      });
    }),
    http.post(root + "/files/delete", async ({ request }) => {
      expect(await request.json()).toEqual({
        kind: "wakers",
        id: "lumi",
        fileId: "Agents/lumi/notes.md",
        version: "generation",
      });
      removed = true;
      return HttpResponse.json({});
    }),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: /lumi/ }));
  const chrome = screen.getByRole("banner", { name: "设置顶栏" });
  expect(await within(chrome).findByText("lumi")).toHaveAttribute(
    "aria-current",
    "page",
  );
  expect(await screen.findByRole("heading", { name: "lumi" })).toHaveClass(
    "sr-only",
  );
  expect(await screen.findByText("notes.md")).toBeVisible();
  const search = screen.getByRole("searchbox", { name: "搜索文件" });
  const sort = screen.getByRole("combobox", { name: "文件排序" });
  expect(
    sort.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "notes.md 的操作" }));
  await user.click(await screen.findByRole("menuitem", { name: "删除文件" }));
  const dialog = await screen.findByRole("dialog", { name: "删除文件？" });
  expect(removed).toBe(false);
  await user.click(within(dialog).getByRole("button", { name: "删除文件" }));
  expect(await screen.findByText("没有文件。")).toBeVisible();
  expect(removed).toBe(true);
  await user.click(within(chrome).getByRole("link", { name: "存储" }));
  expect(await screen.findByRole("heading", { name: "伙伴" })).toBeVisible();
  expect(screen.getByLabelText("当前地址")).toHaveTextContent(
    /^\/settings\/storage$/,
  );
});

it("直接打开伙伴会话页签时，加载前可返回总览并在读回后显示伙伴名称", async () => {
  const files = vi.fn();
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(root + "/files", () => {
      files();
      return HttpResponse.json({ items: [] });
    }),
    http.get("http://127.0.0.1:8080/api/v1/wakers/lumi", async () => {
      await gate;
      return HttpResponse.json({ id: "lumi", name: "Lumi" });
    }),
  );
  openSettings("/settings/storage?files=wakers&owner=lumi&tab=chats");
  const chrome = screen.getByRole("banner", { name: "设置顶栏" });
  const overview = await within(chrome).findByRole("link", { name: "存储" });
  expect(overview).toHaveAttribute("href", "/settings/storage");
  expect(within(chrome).getByText("伙伴")).toHaveAttribute(
    "aria-current",
    "page",
  );
  release();
  expect(await within(chrome).findByText("Lumi")).toHaveAttribute(
    "aria-current",
    "page",
  );
  expect(screen.getByRole("radio", { name: "会话" })).toBeChecked();
  expect(files).not.toHaveBeenCalled();
});

it("伙伴会话页签只读取本伙伴，确认删除后刷新占用列表", async () => {
  let deleted = false;
  server.use(
    http.get(root + "/files", () =>
      HttpResponse.json({ title: "Lumi", items: [], total: 0, complete: true }),
    ),
    http.get(root + "/entities", ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("kind") === "wakers") {
        return HttpResponse.json({
          items: [{ id: "lumi", title: "Lumi", bytes: 4096 }],
          total: 1,
        });
      }
      if (params.get("wakerId") !== "lumi")
        return HttpResponse.json({ items: [], total: 0 });
      return HttpResponse.json({
        items: [
          ...(!deleted
            ? [{ id: "large", title: "大型会话", bytes: 3072, wakerId: "lumi" }]
            : []),
          { id: "small", title: "小型会话", bytes: 1024, wakerId: "lumi" },
        ],
        total: deleted ? 1 : 2,
      });
    }),
    http.delete("http://127.0.0.1:8080/api/v1/chats/large", () => {
      deleted = true;
      return HttpResponse.json({});
    }),
  );
  const user = openSettings();
  await user.click(await screen.findByText("Lumi"));
  await user.click(await screen.findByRole("radio", { name: "会话" }));
  const larger = await screen.findByText("大型会话");
  const smaller = screen.getByText("小型会话");
  expect(
    larger.compareDocumentPosition(smaller) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(screen.queryByRole("searchbox", { name: "搜索文件" })).toBeNull();
  const opener = screen.getByRole("button", { name: /删除.*大型会话/ });
  await user.click(opener);
  await screen.findByRole("dialog", { name: "删除对话" });
  await user.keyboard("{Escape}");
  await waitFor(() => expect(opener).toHaveFocus());
  await user.keyboard("{Enter}");
  const dialog = await screen.findByRole("dialog", { name: "删除对话" });
  expect(deleted).toBe(false);
  await user.click(within(dialog).getByRole("button", { name: "删除对话" }));
  await waitFor(() => expect(screen.queryByText("大型会话")).toBeNull());
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: /删除.*小型会话/ }),
    ).toHaveFocus(),
  );
  expect(deleted).toBe(true);
  expect(screen.getByLabelText("当前地址")).toHaveTextContent("tab=chats");
});

it("文件续页游标失效时重新获取游标并保留搜索和排序", async () => {
  const cursors: string[] = [];
  let fresh = false;
  server.use(
    http.get("http://127.0.0.1:8080/api/v1/wakers/lumi", () =>
      HttpResponse.json({ id: "lumi", name: "Lumi" }),
    ),
    http.get(root + "/files", ({ request }) => {
      const params = new URL(request.url).searchParams;
      expect(params.get("search")).toBe("report");
      expect(params.get("sort")).toBe("largest");
      const cursor = params.get("cursor") ?? "";
      cursors.push(cursor);
      if (cursor === "old-tail") {
        fresh = true;
        return HttpResponse.json(
          { key: "StorageSnapshotExpired", message: "expired" },
          { status: 409 },
        );
      }
      const relativePath = cursor ? "report-end.txt" : "report-head.txt";
      return HttpResponse.json({
        title: "Lumi",
        total: 2,
        complete: true,
        items: [{ id: relativePath, relativePath, source: "workspace" }],
        nextCursor: cursor ? undefined : fresh ? "new-tail" : "old-tail",
      });
    }),
  );
  const user = openSettings(
    "/settings/storage?files=wakers&owner=lumi&search=report&sort=largest",
  );
  await user.click(await screen.findByRole("button", { name: "查看更多" }));
  expect(await screen.findByText("report-end.txt")).toBeVisible();
  expect(cursors).toEqual(["", "old-tail", "", "new-tail"]);
  expect(screen.getByRole("searchbox")).toHaveValue("report");
  expect(screen.getByRole("combobox", { name: "文件排序" })).toHaveTextContent(
    "占用最大",
  );
});

it("按文件名搜索只请求停顿后的查询", async () => {
  const searches: string[] = [];
  server.use(
    http.get("http://127.0.0.1:8080/api/v1/wakers/lumi", () =>
      HttpResponse.json({ id: "lumi", name: "Lumi" }),
    ),
    http.get(root + "/files", ({ request }) => {
      searches.push(new URL(request.url).searchParams.get("search") ?? "");
      return HttpResponse.json({ total: 0, complete: true, items: [] });
    }),
  );
  const user = openSettings("/settings/storage?files=wakers&owner=lumi");
  await screen.findByText("没有文件。");
  await user.type(screen.getByRole("searchbox"), "report");
  expect(await screen.findByText("没有匹配的文件。")).toBeVisible();
  expect(searches).toEqual(["", "report"]);
});

it("确认和读屏名称区分同名文件，取消和删除均恢复焦点", async () => {
  let removed = false;
  const file = (folder: string) => ({
    id: folder,
    relativePath: folder + "/report.txt",
    source: "workspace",
    version: "v1",
  });
  server.use(
    http.get("http://127.0.0.1:8080/api/v1/wakers/lumi", () =>
      HttpResponse.json({ id: "lumi", name: "Lumi" }),
    ),
    http.get(root + "/files", () =>
      HttpResponse.json({
        total: removed ? 1 : 2,
        complete: true,
        items: removed ? [file("b")] : [file("a"), file("b")],
      }),
    ),
    http.post(root + "/files/delete", async ({ request }) => {
      expect(await request.json()).toMatchObject({ fileId: "a" });
      removed = true;
      return HttpResponse.json({});
    }),
  );
  const user = openSettings("/settings/storage?files=wakers&owner=lumi");
  const opener = await screen.findByRole("button", {
    name: "a/report.txt 的操作",
  });
  async function openFileConfirmation() {
    opener.focus();
    await user.keyboard("{Enter}");
    const remove = await screen.findByRole("menuitem", { name: "删除文件" });
    remove.focus();
    await user.keyboard("{Enter}");
    return screen.findByRole("dialog", { name: "删除文件？" });
  }
  const dialog = await openFileConfirmation();
  expect(dialog).toHaveTextContent("a/report.txt");
  await user.keyboard("{Escape}");
  await waitFor(() => expect(opener).toHaveFocus());
  await user.click(
    within(await openFileConfirmation()).getByRole("button", {
      name: "删除文件",
    }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "b/report.txt 的操作" }),
    ).toHaveFocus(),
  );
});

it("删除同名附件时展示会话和上传标识", async () => {
  server.use(
    http.get(root + "/files", () =>
      HttpResponse.json({
        total: 2,
        complete: true,
        items: ["upload-a1234567", "upload-b1234567"].map((messageId) => ({
          id: messageId,
          relativePath: "attachments/" + messageId + "/report.txt",
          source: "attachment",
          chatTitle: "日报",
          modifiedAt: 1700000000000,
          messageId,
          version: "v1",
        })),
      }),
    ),
  );
  const user = openSettings("/settings/storage?files=wakers&owner=lumi");
  await user.click(
    await screen.findByRole("button", {
      name: /report\.txt.*日报.*上传 b1234567/,
    }),
  );
  await user.click(await screen.findByRole("menuitem", { name: "删除文件" }));
  const dialog = await screen.findByRole("dialog", { name: "删除文件？" });
  expect(dialog).toHaveTextContent("日报");
  expect(dialog).toHaveTextContent("上传 b1234567");
  expect(dialog).not.toHaveTextContent("attachments/");
});

it("清理确认取消返还焦点，断线期间保持确认不可提交", async () => {
  const user = openSettings();
  const opener = await screen.findByRole("button", { name: "清理诊断日志" });
  opener.focus();
  await user.keyboard("{Enter}");
  await screen.findByRole("dialog", { name: "清理所选资源？" });
  await user.keyboard("{Escape}");
  await waitFor(() => expect(opener).toHaveFocus());
  await user.keyboard("{Enter}");
  const dialog = await screen.findByRole("dialog", { name: "清理所选资源？" });
  act(() => {
    for (const observer of events.observers)
      observer.onState?.("reconnecting", false);
  });
  expect(
    within(dialog).getByRole("button", { name: "确认清理" }),
  ).toBeDisabled();
});
