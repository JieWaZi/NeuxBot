import { describe, expect, it } from "vitest";

import { parseProfileProjection } from "./profile-projection";

describe("parseProfileProjection", () => {
  it("只解析三个固定标题并保留条目顺序", () => {
    const projection = parseProfileProjection({
      identity:
        "# 身份\n\n## 擅长领域\n\n### 需求分析\n\n识别目标与约束\n\n### 结果验证\n\n用证据验证交付结果\n\n## 其他\n\n不应解析",
      persona: "## 工作风格\r\n\r\n1. 证据优先\r\n2. 小步迭代",
      playbook:
        "## 工作方式\n\n### 交付任务\n\n1. 理解目标\n2. 执行验证\n\n### 复盘\n\n- 记录结论",
    });

    expect(projection).toEqual({
      expertise: [
        { title: "需求分析", description: "识别目标与约束" },
        { title: "结果验证", description: "用证据验证交付结果" },
      ],
      workStyle: ["证据优先", "小步迭代"],
      workMethods: [
        { name: "交付任务", steps: ["理解目标", "执行验证"] },
        { name: "复盘", steps: ["记录结论"] },
      ],
    });
  });

  it("缺少固定标题时返回空投影", () => {
    expect(
      parseProfileProjection({
        identity: "## 能力\n\n- 不兼容其他标题",
        persona: "",
      }),
    ).toEqual({
      expertise: [],
      workStyle: [],
      workMethods: [],
    });
  });
});
