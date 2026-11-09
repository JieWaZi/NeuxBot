import { expect, it, vi } from "vitest";

it("欢迎阶段只加载基础文案，工作台入口在首次使用前注册完整领域文案", async () => {
  // 公共 setup 已加载工作台文案；重建生产入口，避免它掩盖加载顺序。
  vi.resetModules();
  const { i18n } = await import("@/shared/i18n");
  expect(i18n.t("common:startup.status.connection")).toBe("正在连接本地服务");
  for (const namespace of ["workflow", "automaticTask"]) {
    expect(i18n.hasResourceBundle("zh-CN", namespace)).toBe(false);
    expect(i18n.hasResourceBundle("en-US", namespace)).toBe(false);
  }

  // 导入真实工作台入口，包含路由与 Provider，不能由测试预注册代替。
  await import("./workspace");
  const [{ workflowZh, workflowEn }, { automaticTaskZh, automaticTaskEn }] =
    await Promise.all([
      import("@/shared/i18n/workflow"),
      import("@/shared/i18n/automatic-task"),
    ]);
  for (const [namespace, zh, en] of [
    ["workflow", workflowZh, workflowEn],
    ["automaticTask", automaticTaskZh, automaticTaskEn],
  ] as const) {
    expect(i18n.getResourceBundle("zh-CN", namespace)).toEqual(zh);
    expect(i18n.getResourceBundle("en-US", namespace)).toEqual(en);
  }
  expect(i18n.getFixedT("zh-CN", "workflow")("title")).toBe("工作流");
  expect(i18n.getFixedT("en-US", "workflow")("title")).toBe("Workflows");
  expect(i18n.getFixedT("zh-CN", "automaticTask")("title")).toBe("自动任务");
  expect(i18n.getFixedT("en-US", "automaticTask")("title")).toBe(
    "Automatic tasks",
  );
  expect(i18n.getFixedT("fr", "workflow")("title")).toBe("工作流");
  expect(i18n.getFixedT("fr", "automaticTask")("title")).toBe("自动任务");
});
