import { describe, expect, it } from "vitest";

import { ApiError } from "./errors";
import {
  ALLY_LOCAL_ERROR_SURFACE,
  hasLocalErrorSurface,
  localApiErrorMessage,
  withLocalErrorSurface,
} from "./local-error-surface";

describe("local error surface", () => {
  it("标记就地错误并保留原有 headers", () => {
    expect(
      withLocalErrorSurface({
        path: { id: "1" },
        headers: { Authorization: "Bearer x" },
      }),
    ).toEqual({
      path: { id: "1" },
      headers: {
        Authorization: "Bearer x",
        [ALLY_LOCAL_ERROR_SURFACE]: "local",
      },
    });
  });

  it("识别 options 与 Request 上的就地标记", () => {
    const options = withLocalErrorSurface({});
    expect(hasLocalErrorSurface(options)).toBe(true);
    const request = new Request("http://localhost/api", {
      headers: { [ALLY_LOCAL_ERROR_SURFACE]: "local" },
    });
    expect(hasLocalErrorSurface(undefined, request)).toBe(true);
    expect(hasLocalErrorSurface({})).toBe(false);
  });

  it("优先返回 ApiError 文案", () => {
    expect(
      localApiErrorMessage(new ApiError("读取失败", "ReadFailed"), "失败"),
    ).toBe("读取失败");
    expect(localApiErrorMessage(new Error("x"), "失败")).toBe("失败");
  });
});
