import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { client } from "@/shared/api/generated/client.gen";
import { useWorkspaceAttention } from "./queries";

const transport = vi.hoisted(() => ({
  epoch: undefined as string | undefined,
  observers: new Set<GlobalEventObserver>(),
}));

vi.mock("@/shared/api/global-events", () => ({
  getReadyEventEpoch: () => transport.epoch,
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    transport.observers.add(observer);
    return () => transport.observers.delete(observer);
  },
}));

function ready(epoch: string) {
  transport.epoch = epoch;
  act(() => {
    for (const observer of transport.observers)
      observer.onEvent({
        protocolVersion: 1,
        epoch,
        connectionId: epoch,
        deliverySequence: 1,
        topic: "work-board",
        type: "work-board.ready",
        resource: { kind: "global" },
        eventId: "ready",
        payload: {},
      });
  });
}

it("重新握手后拒绝旧进程结果，且不完整读取保留已知关注", async () => {
  const origin = "http://workspace-attention.test";
  const cache = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={cache}>{children}</QueryClientProvider>
  );
  let authority = {
    hasAction: true,
    hasUnread: false,
    complete: true,
    epoch: "old",
    incompleteReason: "",
  };
  const get = vi.fn(async () => HttpResponse.json(authority));
  const server = setupServer(
    http.get(`${origin}/api/v1/workspace-attention`, get),
  );
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: origin });
  transport.epoch = undefined;
  const view = renderHook(() => useWorkspaceAttention(), { wrapper });
  try {
    await waitFor(() =>
      expect(view.result.current).toEqual({
        hasAction: true,
        hasUnread: false,
        synced: false,
      }),
    );
    ready("old");
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(view.result.current.synced).toBe(true));
    transport.epoch = undefined;
    act(() => {
      for (const observer of transport.observers)
        observer.onState?.("reconnecting");
    });
    expect(view.result.current).toEqual({
      hasAction: true,
      hasUnread: false,
      synced: false,
    });

    let finishRead = () => {};
    const delayed = new Promise<void>((resolve) => {
      finishRead = resolve;
    });
    get.mockImplementationOnce(async () => {
      await delayed;
      return HttpResponse.json(authority);
    });
    try {
      ready("old");
      expect(view.result.current.synced).toBe(false);
      await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
      expect(view.result.current.synced).toBe(false);
    } finally {
      finishRead();
    }
    await waitFor(() => expect(view.result.current.synced).toBe(true));

    authority = { ...authority, hasAction: false };
    ready("new");
    await waitFor(() => expect(get).toHaveBeenCalledTimes(4));
    expect(view.result.current).toEqual({
      hasAction: true,
      hasUnread: false,
      synced: false,
    });

    authority = {
      hasAction: false,
      hasUnread: true,
      complete: false,
      epoch: "new",
      incompleteReason: "isolated",
    };
    ready("new");
    await waitFor(() =>
      expect(view.result.current).toEqual({
        hasAction: true,
        hasUnread: true,
        synced: false,
      }),
    );

    authority = {
      ...authority,
      complete: true,
      hasUnread: false,
      incompleteReason: "",
    };
    ready("new");
    await waitFor(() =>
      expect(view.result.current).toEqual({
        hasAction: false,
        hasUnread: false,
        synced: true,
      }),
    );
  } finally {
    view.unmount();
    cache.clear();
    server.close();
    transport.epoch = undefined;
  }
});
