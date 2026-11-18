import type { GlobalEventObserver } from "@/shared/api/global-events";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { platform, configurePlatform } from "@/shared/platform/provider";
import { client } from "@/shared/api/generated/client.gen";
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
  http.get(root + "/usage", () => HttpResponse.json(usage)),
  http.get(root + "/entities", () =>
    HttpResponse.json({ items: [], total: 0 }),
  ),
  http.get(root + "/policy", () => HttpResponse.json(policy)),
  http.get(root + "/maintenance", () => HttpResponse.json({})),
  http.get(root + "/expired-chats", () =>
    HttpResponse.json({
      items: [
        {
          chatId: "c1",
          wakerId: "w1",
          wakerName: "Lumi",
          title: "Archive",
          lastActivityAt: 1700000000000,
          bytes: 1024,
          files: [
            {
              category: "attachments",
              path: "/work/chat/attachments",
              bytes: 1024,
            },
          ],
        },
      ],
      total: 1,
      complete: true,
      knownBytes: 1024,
    }),
  ),
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
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function openSettings() {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter>
        <SettingsScene
          section={settingsSectionOf("storage")}
          onClose={() => {}}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return user;
}

it("在部分统计中保留已知大小，展开分类后可读真实路径", async () => {
  const user = openSettings();
  expect(await screen.findByText(/至少/)).toBeVisible();
  await user.click(screen.getByText("工作文件"));
  expect(screen.getByText("/work/Wakers")).toBeVisible();
  expect(screen.queryByText("桌面缓存")).toBeNull();
});

it("首次启用以拟保存期限签发预览，取消不写入，确认携带预览和原版本", async () => {
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
  expect(writes).toEqual([]);
  await user.click(toggle);
  dialog = await screen.findByRole("dialog");
  await user.click(
    await within(dialog).findByRole("button", { name: "确认启用" }),
  );
  expect(previews).toEqual([{ retentionDays: 15 }, { retentionDays: 15 }]);
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
});

it("手动删除冻结选择，预览展示私有目录并独立确认", async () => {
  const previews: unknown[] = [],
    writes: unknown[] = [];
  server.use(
    http.post(root + "/expired-chats/preview", async ({ request }) => {
      previews.push(await request.json());
      return HttpResponse.json({
        id: "manual-1",
        total: 1,
        knownBytes: 1024,
        complete: true,
      });
    }),
    http.post(root + "/expired-chats/cleanup", async ({ request }) => {
      writes.push(await request.json());
      return HttpResponse.json({ runId: "r1" });
    }),
  );
  const user = openSettings();
  await user.click(
    await screen.findByRole("button", { name: "预览待清理会话" }),
  );
  await user.click(await screen.findByText("Archive"));
  expect(screen.getByText("/work/chat/attachments")).toBeVisible();
  await user.click(screen.getByRole("checkbox", { name: "选择 Archive" }));
  await user.click(screen.getByRole("button", { name: "删除所选会话" }));
  const dialog = await screen.findByRole("dialog", {
    name: "永久删除所选会话？",
  });
  expect(writes).toEqual([]);
  await user.click(
    await within(dialog).findByRole("button", { name: "永久删除" }),
  );
  await waitFor(() => expect(writes).toEqual([{ previewId: "manual-1" }]));
  expect(previews).toEqual([{ retentionDays: 15, chatIds: ["c1"] }]);
});

it("部分成功时仅重新预览失败类别，重新签发的标识用于提交", async () => {
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
  expect(await within(dialog).findByText("历史日志")).toBeVisible();
  expect(reviews).toContainEqual({ categories: ["logs"] });
  expect(writes).toEqual([]);
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  await waitFor(() => expect(writes).toEqual([{ previewId: "new-resource" }]));
});

it("预览行保留会话后重新读取候选", async () => {
  let kept = false;
  server.use(
    http.put(root + "/chats/c1/protection", async ({ request }) => {
      kept = ((await request.json()) as { protected: boolean }).protected;
      return HttpResponse.json({});
    }),
    http.get(root + "/expired-chats", () =>
      HttpResponse.json({
        items: kept
          ? []
          : [
              {
                chatId: "c1",
                title: "Keep",
                wakerId: "w1",
                bytes: 5,
                files: [],
              },
            ],
        total: kept ? 0 : 1,
        knownBytes: kept ? 0 : 5,
        complete: true,
      }),
    ),
  );
  const user = openSettings();
  await user.click(
    await screen.findByRole("button", { name: "预览待清理会话" }),
  );
  await user.click(await screen.findByRole("button", { name: "保留此会话" }));
  expect(await screen.findByText("当前没有符合条件的会话。")).toBeVisible();
});

it("桌面日志失败后仅重试该类别，保留成功的桌面缓存回执", async () => {
  const calls: string[] = [];
  const original = platform();
  configurePlatform({
    ...original,
    kind: "desktop",
    host: {
      ...original.host,
      preview_desktop_storage: async () => ({
        items: [
          { category: "desktop-cache", bytes: 512 },
          { category: "desktop-logs", bytes: 256 },
        ],
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
  await within(dialog).findByText("桌面缓存");
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  await user.click(await screen.findByRole("button", { name: "重试失败类别" }));
  dialog = await screen.findByRole("dialog");
  await within(dialog).findByText("桌面日志");
  expect(within(dialog).queryByText("桌面缓存")).toBeNull();
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  await waitFor(() =>
    expect(calls).toEqual(["desktop-cache", "desktop-logs", "desktop-logs"]),
  );
});

it("较大对话默认读取五项，查看更多沿服务端游标追加", async () => {
  const requests: string[] = [];
  server.use(
    http.get(root + "/entities", ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("kind") !== "chats")
        return HttpResponse.json({ items: [], total: 0 });
      requests.push(params.get("limit") + ":" + (params.get("cursor") ?? ""));
      const more = params.has("cursor");
      return HttpResponse.json({
        items: (more ? [6] : [1, 2, 3, 4, 5]).map((id) => ({
          id: "chat-" + id,
          title: "Chat " + id,
          wakerId: "lumi",
          wakerName: "Lumi",
          bytes: 100 - id,
        })),
        total: 6,
        nextCursor: more ? undefined : "chat-tail",
      });
    }),
  );
  const user = openSettings();
  expect(await screen.findByRole("button", { name: /Chat 5/ })).toBeVisible();
  expect(screen.queryByText("Chat 6")).toBeNull();
  await user.click(screen.getByRole("button", { name: "查看更多" }));
  expect(await screen.findByRole("button", { name: /Chat 6/ })).toBeVisible();
  expect(requests).toEqual(["5:", "5:chat-tail"]);
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

it("已启用时缩短保留期预览拟保存的天数，取消后仍显示已保存期限", async () => {
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
  expect(await within(dialog).findByText(/至少 100 B/)).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(screen.getByRole("combobox", { name: "保留天数" })).toHaveTextContent(
    "60 天",
  );
  expect(previews).toEqual([{ retentionDays: 30 }]);
});

it("过期会话大小未知时不把零当作精确测量", async () => {
  server.use(
    http.get(root + "/expired-chats", () =>
      HttpResponse.json({
        items: [
          {
            chatId: "unknown",
            title: "Unknown",
            bytes: 0,
            sizeKnown: false,
            files: [],
          },
          {
            chatId: "partial",
            title: "Partial",
            bytes: 8,
            sizeKnown: false,
            files: [],
          },
        ],
        total: 2,
        knownBytes: 8,
        complete: false,
      }),
    ),
  );
  const user = openSettings();
  await user.click(
    await screen.findByRole("button", { name: "预览待清理会话" }),
  );
  const title = await screen.findByText("Unknown");
  expect(title.parentElement).toHaveTextContent("大小未知");
  expect(screen.getByText("Partial").parentElement).toHaveTextContent(
    "至少 8 B",
  );
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
  server.use(
    http.post(root + "/cleanup", () =>
      HttpResponse.json({ runId: "just-accepted" }),
    ),
  );
  const user = openSettings();
  await user.click(await screen.findByRole("button", { name: "一键清理" }));
  const dialog = await screen.findByRole("dialog", { name: "清理所选资源？" });
  await within(dialog).findByText("历史日志");
  await user.click(within(dialog).getByRole("button", { name: "确认清理" }));
  expect(
    await screen.findByText("请求已接纳，正在读取维护状态。"),
  ).toBeVisible();
});
