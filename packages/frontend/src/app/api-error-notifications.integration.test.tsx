import { StrictMode } from "react";
import { act, cleanup, render, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import type {
  GlobalEventFrame,
  GlobalEventObserver,
} from "@/shared/api/global-events";
import {
  dismissErrorNotices,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { ApiErrorNotifications } from "./api-error-notifications";

const fixture = vi.hoisted(() => ({
  observers: new Set<GlobalEventObserver>(),
  error: vi.fn(),
  dismiss: vi.fn(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    fixture.observers.add(observer);
    return () => {
      fixture.observers.delete(observer);
    };
  },
}));
vi.mock("@/shared/ui/sonner", () => ({
  toast: { error: fixture.error, dismiss: fixture.dismiss },
}));
afterEach(() => {
  cleanup();
  dismissErrorNotices(["plugins:catalog-sync"]);
  fixture.error.mockClear();
});
function failure(eventId = "sync-failed-1"): GlobalEventFrame {
  return {
    protocolVersion: 1,
    epoch: "core-1",
    connectionId: "connection-1",
    deliverySequence: 1,
    topic: "plugins",
    type: "plugins.catalog.sync.failed",
    resource: { kind: "global" },
    eventId,
    payload: { errorCode: "PluginDistributionUnavailable" },
  };
}
it("reports one global toast per real background failure and ignores resync snapshots", () => {
  render(
    <StrictMode>
      <ApiErrorNotifications />
    </StrictMode>,
  );
  const notices = renderHook(useErrorNotices);
  const frame = failure();
  act(() => {
    for (const observer of fixture.observers) {
      observer.onEvent(frame);
      observer.onEvent(frame);
      observer.onResync?.("reconnected");
      observer.onEvent({ ...frame, type: "plugins.ready" });
    }
  });
  expect(notices.result.current).toHaveLength(1);
  expect(notices.result.current[0]).toMatchObject({
    scope: "global",
    errorCode: "PluginDistributionUnavailable",
  });
  expect(fixture.error).toHaveBeenCalledOnce();
  expect(fixture.error.mock.calls[0]?.[0]).toBe(
    "插件目录或安装包暂时无法下载，请检查网络后重试",
  );
  act(() => {
    for (const observer of fixture.observers)
      observer.onEvent(failure("sync-failed-2"));
  });
  expect(notices.result.current).toHaveLength(1);
  expect(fixture.error).toHaveBeenCalledTimes(2);
  expect(
    JSON.parse(localStorage.getItem("ally:error-notices") ?? "[]"),
  ).toEqual([]);
  act(() => {
    for (const observer of fixture.observers)
      observer.onEvent({
        ...failure("sync-succeeded"),
        type: "plugins.catalog.sync.succeeded",
      });
  });
  expect(notices.result.current).toHaveLength(0);
  expect(fixture.dismiss).toHaveBeenCalledWith("plugins:catalog-sync");
});
