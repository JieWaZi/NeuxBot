import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, renderHook, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { useRef, type ReactNode } from "react";
import {
  captureChatOpenIntent,
  markLatestChatUnread,
  useChatReading,
  usePresentedChatBoundary,
} from "./chat-reading";
import { chatReadingOptions, type ChatReadingState } from "./reading-queries";
import { client } from "./generated/client.gen";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

vi.mock("./global-events", () => ({
  getReadyEventEpoch: () => undefined,
  subscribeGlobalEvents: () => () => {},
}));

it("侧栏标未读使用最新可读消息和服务端代次，并保持已有手动未读", async () => {
  const queryClient = new QueryClient();
  const chatId = "chat";
  const base = "http://127.0.0.1:19923/api/v1/chats/chat";
  let authority: ChatReadingState = {
    chatId,
    throughSequence: 12,
    unreadGeneration: 3,
    holdUnread: false,
    unreadMessageCount: 0,
    latestReadableMessageID: "last-reply",
    complete: true,
  };
  const writes: unknown[] = [];
  const server = setupServer(
    http.get(base + "/reading", () => HttpResponse.json(authority)),
    http.post(base + "/unread", async ({ request }) => {
      writes.push(await request.json());
      authority = {
        ...authority,
        throughSequence: 11,
        unreadGeneration: 4,
        holdUnread: true,
        unreadMessageCount: 1,
        firstUnreadMessageID: "last-reply",
      };
      return HttpResponse.json({});
    }),
  );
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://127.0.0.1:19923" });
  try {
    await expect(markLatestChatUnread(queryClient, chatId)).resolves.toBe(true);
    await expect(markLatestChatUnread(queryClient, chatId)).resolves.toBe(true);
    expect(writes).toEqual([
      { fromMessageID: "last-reply", observedUnreadGeneration: 3 },
    ]);
    expect(
      queryClient.getQueryData(chatReadingOptions(chatId).queryKey),
    ).toMatchObject({
      holdUnread: true,
      unreadGeneration: 4,
    });
  } finally {
    queryClient.clear();
    server.close();
  }
});

it("在途的显式打开发生代次冲突后只重读，不重放旧动作", async () => {
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  const chatId = "chat";
  const path = "http://127.0.0.1:19923/api/v1/chats/chat/reading";
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const options = chatReadingOptions(chatId);
  let authority: ChatReadingState = {
    chatId,
    throughSequence: 0,
    unreadGeneration: 2,
    holdUnread: true,
    unreadMessageCount: 3,
    latestReadableMessageID: "message",
    complete: true,
    epoch: "core",
  };
  queryClient.setQueryData(options.queryKey, authority);
  const writes: unknown[] = [];
  const server = setupServer(
    http.get(path, () => HttpResponse.json(authority)),
    http.post(path, async ({ request }) => {
      const body = await request.json();
      writes.push(body);
      return writes.length === 1
        ? HttpResponse.json({ key: "ReadingIntentConflict" }, { status: 409 })
        : HttpResponse.json({});
    }),
  );
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://127.0.0.1:19923" });
  const navigationState = captureChatOpenIntent(queryClient, chatId);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter
        initialEntries={[{ pathname: "/chat", state: navigationState }]}
      >
        {children}
      </MemoryRouter>
    </QueryClientProvider>
  );
  const hook = renderHook(() => useChatReading({ chatId, active: true }), {
    wrapper,
  });
  try {
    authority = { ...authority, unreadGeneration: 3 };
    act(() =>
      hook.result.current.present({ messageId: "message", sequence: 7 }),
    );
    await waitFor(() =>
      expect(hook.result.current.state?.unreadGeneration).toBe(3),
    );
    act(() =>
      hook.result.current.present({ messageId: "message", sequence: 7 }),
    );
    expect(writes).toEqual([
      {
        intent: "explicit",
        throughMessageID: "message",
        observedUnreadGeneration: 2,
      },
    ]);
    await act(() => hook.result.current.markRead());
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual({
      intent: "explicit",
      throughMessageID: "message",
      observedUnreadGeneration: 3,
    });
  } finally {
    hook.unmount();
    queryClient.clear();
    server.close();
    focus.mockRestore();
  }
});

it("实际显示的持久正文才可读取，预取、视区外、后台以及临时正文均不确认", async () => {
  let notify: IntersectionObserverCallback = () => {};
  const targets = new Set<Element>();
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  const layout = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockImplementation(function (this: HTMLElement) {
      return this.dataset.outside
        ? new DOMRect(0, 1200, 100, 20)
        : new DOMRect(0, 0, 100, 20);
    });
  class Intersection {
    constructor(callback: IntersectionObserverCallback) {
      notify = callback;
    }
    observe(target: Element) {
      targets.add(target);
    }
    unobserve(target: Element) {
      targets.delete(target);
    }
    disconnect() {
      targets.clear();
    }
  }
  vi.stubGlobal("IntersectionObserver", Intersection);
  const present = vi.fn();
  const showRows = () => {
    const entries = [...targets].map((target) => ({
      target,
      isIntersecting: true,
      intersectionRect: new DOMRect(0, 0, 100, 20),
    }));
    act(() =>
      notify(
        entries as IntersectionObserverEntry[],
        {} as IntersectionObserver,
      ),
    );
  };
  function Body({ active }: { active: boolean }) {
    const root = useRef<HTMLDivElement>(null);
    usePresentedChatBoundary({ root, active, present, revision: 1 });
    return (
      <div ref={root}>
        <div data-message-id="visible" data-chat-readable-sequence={10}>
          <p data-chat-readable-body>持久公开正文</p>
        </div>
        <div data-message-id="outside" data-chat-readable-sequence={12}>
          <p data-outside="true" data-chat-readable-body>
            屏幕外正文
          </p>
        </div>
        <div data-message-id="transient">
          <p data-chat-readable-body>临时正文</p>
        </div>
      </div>
    );
  }
  const view = render(<Body active={false} />);
  try {
    showRows();
    expect(present).not.toHaveBeenCalled();
    view.rerender(<Body active />);
    expect(present).not.toHaveBeenCalled();
    focus.mockReturnValue(false);
    showRows();
    expect(present).not.toHaveBeenCalled();
    focus.mockReturnValue(true);
    showRows();
    expect(present.mock.calls).toEqual([
      [{ messageId: "visible", sequence: 10 }],
    ]);
  } finally {
    view.unmount();
    layout.mockRestore();
    focus.mockRestore();
    vi.unstubAllGlobals();
  }
});
