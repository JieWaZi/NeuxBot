import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ExternalToast } from "sonner";
import { ApiErrorNotifications } from "./api-error-notifications";

const { error, dismissErrorNotice, dismissToast, notices } = vi.hoisted(() => ({
  error: vi.fn(),
  dismissErrorNotice: vi.fn(),
  dismissToast: vi.fn(),
  notices: {
    current: [{ id: "api-error", scope: "global", message: "保存失败" }],
  },
}));
vi.mock("@/shared/ui/sonner", () => ({
  toast: { error, dismiss: dismissToast },
}));
vi.mock("@/shared/api/error-notices", () => ({
  dismissErrorNotice,
  useErrorNotices: () => notices.current,
}));

describe("API 错误通知", () => {
  it("对应操作恢复后立即撤掉当前错误 Toast", () => {
    const { rerender } = render(<ApiErrorNotifications />);
    notices.current = [];
    rerender(<ApiErrorNotifications />);
    expect(dismissToast).toHaveBeenCalledWith("api-error");
    notices.current = [
      { id: "api-error", scope: "global", message: "保存失败" },
    ];
  });
  it("自动关闭和滑动移除都会清理错误提示记录", () => {
    render(<ApiErrorNotifications />);
    const options: ExternalToast = error.mock.lastCall?.[1];
    expect(options.duration).toBeUndefined();
    const notice = { id: "api-error" };
    options.onAutoClose?.(notice);
    expect(dismissErrorNotice).toHaveBeenCalledWith("api-error");
    dismissErrorNotice.mockClear();
    options.onDismiss?.(notice);
    expect(dismissErrorNotice).toHaveBeenCalledWith("api-error");
  });
});
