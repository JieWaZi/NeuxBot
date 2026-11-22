import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import "@/app/workspace-i18n";
import type { toast } from "@/shared/ui/sonner";
import { ApiErrorNotifications } from "./api-error-notifications";

const { error, dismissErrorNotice, dismissToast, notices } = vi.hoisted(() => ({
  error: vi.fn(),
  dismissErrorNotice: vi.fn(),
  dismissToast: vi.fn(),
  notices: {
    current: [
      {
        id: "api-error",
        version: "failure-1",
        scope: "global",
        message: "保存失败",
      },
    ],
  },
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: () => () => {},
}));
vi.mock("@/shared/ui/sonner", () => ({
  toast: { error, dismiss: dismissToast },
}));
vi.mock("@/shared/api/error-notices", () => ({
  dismissErrorNotice,
  reportErrorNotice: vi.fn(),
  useErrorNotices: () => notices.current,
}));

describe("API 错误通知", () => {
  it("对应操作恢复后立即撤掉当前错误 Toast", () => {
    const { rerender } = render(<ApiErrorNotifications />);
    notices.current = [];
    rerender(<ApiErrorNotifications />);
    expect(dismissToast).toHaveBeenCalledWith("api-error");
    notices.current = [
      {
        id: "api-error",
        version: "failure-1",
        scope: "global",
        message: "保存失败",
      },
    ];
  });
  it("只为当前失败启动计时，关闭时按版本清理记录", () => {
    error.mockClear();
    const { rerender } = render(<ApiErrorNotifications />);
    const options: Parameters<typeof toast.error>[1] = error.mock.lastCall?.[1];
    notices.current = [...notices.current];
    rerender(<ApiErrorNotifications />);
    expect(error).toHaveBeenCalledExactlyOnceWith(
      "请求失败",
      expect.objectContaining({ id: "api-error", description: "保存失败" }),
    );
    options?.onAutoClose?.();
    expect(dismissErrorNotice).toHaveBeenCalledWith("api-error", "failure-1");
    dismissErrorNotice.mockClear();
    options?.onDismiss?.();
    expect(dismissErrorNotice).toHaveBeenCalledWith("api-error", "failure-1");
  });
});
