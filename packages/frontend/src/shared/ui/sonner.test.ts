import { describe, expect, it, vi } from "vitest";
import { toast } from "./sonner";
const { error } = vi.hoisted(() => ({ error: vi.fn() }));
vi.mock("sonner", () => ({ toast: Object.assign(vi.fn(), { error }) }));

describe("错误通知自动消失", () => {
  it("将常驻错误改为默认四秒，不显示关闭按钮", () => {
    toast.error("保存失败", { duration: Infinity, closeButton: true });
    expect(error).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: 4000, closeButton: false }),
    );
  });

  it("保留调用方指定的有限显示时长", () => {
    toast.error("保存失败", { duration: 1000 });
    expect(error).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: 1000 }),
    );
  });
});
