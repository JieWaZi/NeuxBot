import { afterEach, describe, expect, it } from "vitest";

import { i18n } from "@/shared/i18n";
import { runtimeFailurePresentation } from "./runtime-failure-presentation";

describe("runtimeFailurePresentation", () => {
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

  it("提供英文错误展示文案", async () => {
    await i18n.changeLanguage("en-US");

    expect(runtimeFailurePresentation("QUOTA_EXHAUSTED")).toEqual({
      message:
        "Your current quota has been exhausted. Switch models, or retry after your quota resets.",
    });
  });
});
