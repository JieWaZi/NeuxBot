import { describe, expect, it } from "vitest";

import { ApiError, apiErrorMessageKey, toApiError } from "./errors";

describe("toApiError", () => {
  it("保留后端稳定错误码", () => {
    const error = toApiError(
      { key: "WakerNotFound", message: "伙伴不存在" },
      404,
    );
    expect(error).toBeInstanceOf(ApiError);
    expect(error.code).toBe("WakerNotFound");
    expect(error.status).toBe(404);
  });

  it("将网络异常归一化", () => {
    expect(toApiError(new Error("连接失败")).code).toBe("NETWORK");
  });

  it.each([
    "Failed to fetch",
    "Load failed",
    "NetworkError when attempting to fetch resource.",
  ])("不向用户展示底层网络异常：%s", (message) => {
    const error = toApiError(new TypeError(message));
    expect(error).toMatchObject({
      code: "NETWORK",
      message: "暂时无法连接本地服务，请检查连接状态。",
    });
  });

  it("保留其他错误的具体原因", () => {
    expect(toApiError(new Error("目录不存在")).message).toBe("目录不存在");
  });

  it("识别旧通知中已翻译的连接错误", () => {
    expect(apiErrorMessageKey("暂时无法连接本地服务，请检查连接状态。")).toBe(
      "common:serviceConnectionFailed",
    );
  });
});
