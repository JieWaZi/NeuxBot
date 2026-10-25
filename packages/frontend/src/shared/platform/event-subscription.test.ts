import { describe, expect, it, vi } from "vitest";
import { subscribeDesktopEvent } from "./event-subscription";
import { logger } from "@/shared/diagnostics/logger";

describe("桌面监听生命周期", () => {
  it("模拟卸载发生在注册前时不建立原生订阅", async () => {
    const register = vi.fn(async () => vi.fn());
    const stop = subscribeDesktopEvent("test", register, vi.fn());
    stop();
    await Promise.resolve();
    expect(register).not.toHaveBeenCalled();
  });

  it("注册未完成就卸载时忽略迟到事件，注册完成后只清理一次", async () => {
    let ready: (cleanup: () => void) => void = () => undefined;
    let emit: (payload: string) => void = () => undefined;
    const pending = new Promise<() => void>((resolve) => {
      ready = resolve;
    });
    const cleanup = vi.fn(async () => undefined);
    const onEvent = vi.fn();
    const stop = subscribeDesktopEvent<string>(
      "test",
      (handler) => {
        emit = handler;
        return pending;
      },
      onEvent,
    );
    await Promise.resolve();
    stop();
    stop();
    emit("late");
    ready(cleanup);
    await vi.waitFor(() => expect(cleanup).toHaveBeenCalledOnce());
    expect(onEvent).not.toHaveBeenCalled();
  });

  it("注册和异步注销失败都带阶段日志，不遗留未处理 rejection", async () => {
    const error = vi.spyOn(logger, "error").mockImplementation(() => undefined);
    try {
      subscribeDesktopEvent(
        "failed-registration",
        async () => {
          throw new Error("unavailable");
        },
        vi.fn(),
      );
      const stop = subscribeDesktopEvent(
        "failed-cleanup",
        async () => async () => {
          throw new Error("unavailable");
        },
        vi.fn(),
      );
      await vi.waitFor(() =>
        expect(error).toHaveBeenCalledWith(
          "desktop-event.failed",
          expect.objectContaining({ phase: "subscribe" }),
        ),
      );
      stop();
      await vi.waitFor(() =>
        expect(error).toHaveBeenCalledWith(
          "desktop-event.failed",
          expect.objectContaining({ phase: "unsubscribe" }),
        ),
      );
    } finally {
      error.mockRestore();
    }
  });
});
