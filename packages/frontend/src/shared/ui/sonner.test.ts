import { afterEach, describe, expect, it, vi } from "vitest";
import { toast } from "./sonner";

afterEach(() => {
  toast.dismiss();
  vi.useRealTimers();
});

describe("通知生命周期", () => {
  it("同一错误的新失败版本替换原计时，旧计时不能关闭新错误", () => {
    vi.useFakeTimers();
    const first = vi.fn();
    const second = vi.fn();
    toast.error("保存失败", { id: "save", onAutoClose: first });
    vi.advanceTimersByTime(2000);
    toast.error("再次保存失败", { id: "save", onAutoClose: second });
    vi.advanceTimersByTime(1000);
    expect(first).not.toHaveBeenCalled();
    expect(second).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2000);
    expect(second).toHaveBeenCalledOnce();
  });
});
