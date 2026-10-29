import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { NodeInspector } from "./node-inspector";
const save = vi.hoisted(() => vi.fn(async () => true));
vi.mock("../model/use-workflow-configuration", () => ({
  useWorkflowConfiguration: () => ({ save, pending: false, error: null }),
}));
afterEach(() => save.mockClear());
it("用户决策详情展示业务描述和完整问题，隐藏内部实现", () => {
  render(
    <NodeInspector
      id="flow"
      revisionId="revision"
      readOnly
      node={{
        id: "question-internal",
        kind: "askUser",
        label: "选择方案",
        description: "根据候选方案作出选择",
        inputs: [],
        execution: {
          kind: "select",
          promptTemplate: '"请选择需要分析的方案，确认后伙伴开始分析。"',
        },
      }}
    />,
  );
  expect(screen.getByText("用户决策")).toBeInTheDocument();
  expect(screen.getByText("根据候选方案作出选择")).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Prompt" })).toHaveValue(
    "请选择需要分析的方案，确认后伙伴开始分析。",
  );
  expect(screen.queryByText("question-internal")).not.toBeInTheDocument();
});
it("计算详情保留业务修改入口，不展示代码、摘要或执行模式", async () => {
  render(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        id: "calculate",
        kind: "functionTask",
        label: "整理数据",
        description: "合并用户选择的数据",
        inputs: [],
        execution: {
          language: "python",
          executionMode: "trusted_local",
          codeDigest: "private-digest",
        },
      }}
    />,
  );
  expect(screen.getByText("本地处理")).toBeInTheDocument();
  expect(
    screen.queryByText(/trusted_local|private-digest|python/),
  ).not.toBeInTheDocument();
  const user = userEvent.setup();
  const label = screen.getByRole("textbox", { name: "节点标题" });
  await user.clear(label);
  await user.type(label, "整理销售数据");
  await user.tab();
  expect(save).toHaveBeenCalledWith({
    target: "node",
    id: "calculate",
    fields: { label: "整理销售数据" },
    prompt: undefined,
  });
});

it("脚本详情使用可读风险标签，不泄漏翻译键或内部标识", () => {
  render(
    <NodeInspector
      id="flow"
      revisionId="revision"
      node={{
        id: "private-node",
        kind: "action",
        label: "生成报告",
        inputs: [],
        execution: {
          actionId: "private-action",
          actionType: "script",
          scriptId: "private-script",
        },
      }}
      manifest={{
        formatVersion: 2,
        sdkVersion: "ally.workflow/2",
        title: "报告",
        description: "",
        phases: [],
        nodes: [],
        controls: [],
        edges: [],
        scripts: [],
        actions: [
          {
            id: "private-action",
            label: "生成报告",
            description: "在本地生成报告",
            type: "script",
            risk: "low",
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("操作风险 · 低")).toBeInTheDocument();
  expect(
    screen.queryByText(/nodeOption|private-node|private-action/),
  ).not.toBeInTheDocument();
});
