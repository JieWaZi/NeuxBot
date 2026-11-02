import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor, cleanup, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import "@/shared/i18n";
import { SideChatWorkbenchView } from "./side-chat-workbench-view";

const childLifecycle = vi.hoisted(() => ({ mounts: 0, unmounts: 0 }));
vi.mock("../pages/chat-page", async () => {
  const { useEffect } = await import("react");
  return {
    ChatPageContent: ({ chatId }: { chatId: string }) => {
      useEffect(() => {
        childLifecycle.mounts++;
        return () => {
          childLifecycle.unmounts++;
        };
      }, []);
      return <section>{chatId}</section>;
    },
  };
});

const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://side.test" });
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
});
afterAll(() => server.close());
it("用户创建侧聊时无缓存也能惰性读消息能力并提交真实目标", async () => {
  let reads = 0;
  const posts: unknown[] = [];
  server.use(
    http.get("http://side.test/api/v1/chats/parent/messages", () => {
      reads++;
      return HttpResponse.json({
        resources: [
          {
            id: "inherited",
            executionId: "source",
            inherited: true,
            role: "assistant",
            status: "completed",
            fork: { available: true, latestOnly: true },
          },
        ],
        forkLatestExecutionId: "inherited",
        latestSequence: 4,
      });
    }),
    http.post(
      "http://side.test/api/v1/chats/parent/fork",
      async ({ request }) => {
        posts.push(await request.json());
        return HttpResponse.json({ chatId: "child" });
      },
    ),
  );
  const onChatCreated = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <SideChatWorkbenchView
        parentBusy={false}
        tab={{
          id: "side",
          contentId: "side-content",
          occurrenceId: "side-occurrence",
          navigationRevision: 0,
          kind: "sidechat",
          title: "侧聊",
          parentChatId: "parent",
          createRequestId: "request",
        }}
        onChatCreated={onChatCreated}
      />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(onChatCreated).toHaveBeenCalledWith("child"));
  expect(reads).toBe(1);
  expect(posts).toEqual([
    { executionId: "inherited", clientRequestId: "request" },
  ]);
  view.unmount();
  queryClient.clear();
});

it("父Chat忙碌时不读消息也不从旧可分叉回合POST，空闲后按新head创建", async () => {
  let reads = 0;
  const posts: unknown[] = [];
  server.use(
    http.get("http://side.test/api/v1/chats/parent/messages", () => {
      reads++;
      return HttpResponse.json({
        resources: [
          {
            id: "new",
            executionId: "new",
            role: "assistant",
            status: "completed",
            fork: { available: true },
          },
        ],
        forkLatestExecutionId: "new",
        latestSequence: 9,
      });
    }),
    http.post(
      "http://side.test/api/v1/chats/parent/fork",
      async ({ request }) => {
        posts.push(await request.json());
        return HttpResponse.json({ chatId: "child" });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(["chats", "messages", "parent"], {
    pages: [
      {
        resources: [
          {
            id: "old",
            executionId: "old",
            role: "assistant",
            status: "completed",
            fork: { available: true },
          },
          {
            id: "new",
            executionId: "new",
            role: "assistant",
            status: "running",
            fork: { available: false, reasonCode: "unfinished" },
          },
        ],
        forkLatestExecutionId: "new",
        latestSequence: 6,
      },
    ],
    pageParams: [undefined],
  });
  const onChatCreated = vi.fn();
  const tab = {
    id: "side",
    contentId: "side-content",
    occurrenceId: "side-occurrence",
    navigationRevision: 0,
    kind: "sidechat" as const,
    title: "侧聊",
    parentChatId: "parent",
    createRequestId: "request",
  };
  const content = (parentBusy: boolean) => (
    <QueryClientProvider client={queryClient}>
      <SideChatWorkbenchView
        tab={tab}
        onChatCreated={onChatCreated}
        parentBusy={parentBusy}
      />
    </QueryClientProvider>
  );
  const view = render(content(true));
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(reads).toBe(0);
  expect(posts).toEqual([]);
  view.rerender(content(false));
  await waitFor(() => expect(onChatCreated).toHaveBeenCalledWith("child"));
  expect(reads).toBe(1);
  expect(posts).toEqual([{ executionId: "new", clientRequestId: "request" }]);
  view.unmount();
  queryClient.clear();
});

it("已经绑定的侧聊独立继续，父Chat再忙不会卸载子会话", async () => {
  childLifecycle.mounts = 0;
  childLifecycle.unmounts = 0;
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tab = {
    id: "bound-side",
    contentId: "bound-content",
    occurrenceId: "bound-occurrence",
    navigationRevision: 0,
    kind: "sidechat" as const,
    title: "侧聊",
    chatId: "child",
    parentChatId: "parent",
  };
  const content = (parentBusy: boolean) => (
    <QueryClientProvider client={queryClient}>
      <SideChatWorkbenchView
        tab={tab}
        parentBusy={parentBusy}
        onChatCreated={() => {}}
      />
    </QueryClientProvider>
  );
  const view = render(content(false));
  await screen.findByText("child");
  expect(childLifecycle.mounts).toBe(1);
  view.rerender(content(true));
  expect(childLifecycle.mounts).toBe(1);
  expect(childLifecycle.unmounts).toBe(0);
  view.unmount();
  queryClient.clear();
});
