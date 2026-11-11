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

  it("未知传输异常使用本地化兜底并保留诊断cause", () => {
    const raw = new Error("PrivateTransportType: internal stack");
    const converted = toApiError(raw);
    expect(converted.message).toBe("本地服务返回了无法识别的错误");
    expect(converted.cause).toBe(raw);
  });

  it("识别旧通知中已翻译的连接错误", () => {
    expect(apiErrorMessageKey("暂时无法连接本地服务，请检查连接状态。")).toBe(
      "common:serviceConnectionFailed",
    );
  });
});

it("HTTP 与终态应用错误共用当前语言和安全参数", async () => {
  const { i18n } = await import("@/shared/i18n");
  const failure = toApiError(
    {
      key: "RuntimeWorkspaceScopeUnsupported",
      message: "server fixed language",
      messageSource: "localized",
      params: { MaxDirectories: 2 },
    },
    422,
  );
  expect(failure.message).toBe(
    "当前智能体最多支持 2 个项目目录，请调整工作范围",
  );
  await i18n.changeLanguage("en-US");
  expect(failure.message).toBe(
    "the selected agent supports up to 2 project directories; adjust the workspace scope",
  );
  expect(
    toApiError({
      key: "RuntimeQuotaExhausted",
      message: "Native quota exhausted",
      messageSource: "public",
    }).message,
  ).toBe("Native quota exhausted");
  expect(
    toApiError({
      key: "UnknownPrivateError",
      message: "private implementation text",
      messageSource: "localized",
    }).message,
  ).toBe(i18n.t("common:serviceUnrecognizedError"));
  await i18n.changeLanguage("zh-CN");
});

it("模型确认失败使用当前语言的产品说明并覆盖旧服务文案", async () => {
  const { i18n } = await import("@/shared/i18n");
  const failure = toApiError({
    key: "RuntimeModelSelectionFailed",
    message: "Runtime configuration is invalid",
    messageSource: "localized",
  });
  expect(failure.message).toBe("无法切换到所选模型，请重新选择模型后重试");
  await i18n.changeLanguage("en-US");
  expect(failure.message).toBe(
    "the selected model could not be applied; select a model again and retry",
  );
  await i18n.changeLanguage("zh-CN");
});
