import { describe, expect, it } from "vitest";
import { chatErrorPresentation } from "./chat-error-presentation";

describe("chatErrorPresentation", () => {
  it.each([
    ["Internal error: Free usage limit reached.", "Free usage limit reached."],
    ["RequestError: Internal error: Sign in again.", "Sign in again."],
    ["Internal error: Internal error: Retry later.", "Retry later."],
    [
      "The provider said Internal error: retry later.",
      "The provider said Internal error: retry later.",
    ],
  ])("只去掉协议包装前缀，保留真实原因 %s", (raw, message) => {
    expect(chatErrorPresentation(raw, "未知错误").message).toBe(message);
  });
  it("结构化错误也使用同一可读原因，原始诊断只留在详情", () => {
    const raw = JSON.stringify({
      code: -32603,
      message: "Internal error: Free usage limit reached.",
    });
    expect(chatErrorPresentation(raw, "未知错误")).toEqual({
      message: "Free usage limit reached.",
      details: raw,
    });
  });
});
