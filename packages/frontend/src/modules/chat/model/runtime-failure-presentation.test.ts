import { afterEach, describe, expect, it } from "vitest";

import { i18n } from "@/shared/i18n";
import { runtimeFailurePresentation } from "./runtime-failure-presentation";

describe("runtimeFailurePresentation", () => {
  it("接口给出原生原因时所有已定义错误直接展示该原因", () => {
    for (const code of [
      "QUOTA_EXHAUSTED",
      "AUTH_REQUIRED",
      "MODEL_UNAVAILABLE",
      "INPUT_TIMEOUT",
      "APPROVAL_TIMEOUT",
      "EVENT_BUDGET_EXCEEDED",
    ]) {
      expect(
        runtimeFailurePresentation(
          code,
          "Native CLI rejected this request",
          undefined,
          "public",
        ).message,
      ).toBe("Native CLI rejected this request");
    }
  });
  it("协议错误标识不覆盖可读的失败原因", () => {
    expect(
      runtimeFailurePresentation(
        "QUOTA_EXHAUSTED",
        "[RuntimeExecutionFailed][50000000045]",
      ).message,
    ).toBe("当前额度已用尽。请切换模型，或等待额度恢复后重试。");
  });
  afterEach(async () => {
    await i18n.changeLanguage("zh-CN");
  });

  it("提供用户可处理问题的本地化描述", () => {
    expect(runtimeFailurePresentation("PLAN_UPGRADE_REQUIRED")).toEqual({
      message: "当前模型不在你的套餐范围内。请切换模型，或检查套餐后重试。",
    });
  });

  it("系统故障使用相同消息错误契约", () => {
    expect(runtimeFailurePresentation("PROCESS_EXITED")).toEqual({
      message: "执行进程异常退出，请检查执行结果和运行环境。",
    });
  });

  it("人工等待超时也保留接口给出的原生原因", async () => {
    expect(
      runtimeFailurePresentation(
        "INPUT_TIMEOUT",
        "waiting for user input timed out",
        undefined,
        "public",
      ).message,
    ).toBe("waiting for user input timed out");
    expect(
      runtimeFailurePresentation(
        "APPROVAL_TIMEOUT",
        "waiting for approval timed out",
        undefined,
        "public",
      ).message,
    ).toBe("waiting for approval timed out");
    expect(
      runtimeFailurePresentation(
        "REQUEST_TIMEOUT",
        "provider request deadline",
        undefined,
        "public",
      ).message,
    ).toBe("provider request deadline");
    expect(runtimeFailurePresentation("UNKNOWN", "native detail").message).toBe(
      i18n.t("runtimeFailure.unknown", { ns: "chat" }),
    );
    await i18n.changeLanguage("en-US");
    expect(
      runtimeFailurePresentation(
        "INPUT_TIMEOUT",
        "raw detail",
        undefined,
        "public",
      ).message,
    ).toBe("raw detail");
    expect(
      runtimeFailurePresentation(
        "APPROVAL_TIMEOUT",
        "raw detail",
        undefined,
        "public",
      ).message,
    ).toBe("raw detail");
  });

  it("提供英文错误展示文案", async () => {
    await i18n.changeLanguage("en-US");

    expect(runtimeFailurePresentation("QUOTA_EXHAUSTED")).toEqual({
      message:
        "Your current quota has been exhausted. Switch models, or retry after your quota resets.",
    });
  });

  it("事件预算耗尽保留接口原因，缺失原因时使用已定义分类", async () => {
    expect(
      runtimeFailurePresentation(
        "EVENT_BUDGET_EXCEEDED",
        "limit_exceeded: event count",
        undefined,
        "public",
      ).message,
    ).toBe("limit_exceeded: event count");
    expect(runtimeFailurePresentation("FRAME_TOO_LARGE").message).toBe(
      "返回的数据过大，执行已中止，请检查执行结果。",
    );
    await i18n.changeLanguage("en-US");
    expect(
      runtimeFailurePresentation(
        "EVENT_BUDGET_EXCEEDED",
        "raw diagnostic",
        undefined,
        "public",
      ).message,
    ).toBe("raw diagnostic");
  });
});

describe("application failure contract", () => {
  afterEach(async () => {
    await i18n.changeLanguage("zh-CN");
  });
  it("应用身份按当前语言呈现，包装身份不会成为正文", async () => {
    expect(
      runtimeFailurePresentation("RuntimeEventQueueOverflow").message,
    ).toBe("执行事件未能完整交付，请先检查执行结果");
    expect(
      runtimeFailurePresentation(
        "EVENT_DELIVERY_FAILED",
        "private wrap: [RuntimeEventQueueOverflow][50000000049]\n[RuntimeExecutionFailed][50000000045]",
      ).message,
    ).toBe(i18n.t("runtimeFailure.eventDeliveryFailed", { ns: "chat" }));
    await i18n.changeLanguage("en-US");
    expect(
      runtimeFailurePresentation("RuntimeEventQueueOverflow").message,
    ).toBe(
      "execution events could not be fully delivered; check the execution result before continuing",
    );
  });
  it("参数化错误与历史消息在同一入口本地化", async () => {
    expect(
      runtimeFailurePresentation(
        "RuntimeWorkspaceScopeUnsupported",
        undefined,
        { MaxDirectories: 2 },
      ).message,
    ).toBe("当前智能体最多支持 2 个项目目录，请调整工作范围");
    await i18n.changeLanguage("en-US");
    expect(
      runtimeFailurePresentation(
        "RuntimeWorkspaceScopeUnsupported",
        undefined,
        { MaxDirectories: 2 },
      ).message,
    ).toBe(
      "the selected agent supports up to 2 project directories; adjust the workspace scope",
    );
  });
  it("未知类型名和枚举不能作为未知错误正文", () => {
    expect(
      runtimeFailurePresentation("UNKNOWN", "UnexpectedPrivateType").message,
    ).toBe(i18n.t("runtimeFailure.unknown", { ns: "chat" }));
  });
});

it("旧分类不能为任意诊断提供公开来源", () => {
  for (const diagnostic of [
    "RuntimeEventQueueOverflow",
    "*RuntimeEventQueueOverflow",
    "private implementation failed",
    "unknown payload",
  ]) {
    expect(
      runtimeFailurePresentation("EVENT_DELIVERY_FAILED", diagnostic).message,
    ).toBe(i18n.t("runtimeFailure.eventDeliveryFailed", { ns: "chat" }));
  }
});
