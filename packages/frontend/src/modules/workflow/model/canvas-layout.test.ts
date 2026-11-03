import { expect, it } from "vitest";
import { workflowCanvasLayout, workflowInputNodeWidth } from "./canvas-layout";
import type { Manifest } from "./types";

type Hint = NonNullable<Manifest["controls"]>[number];
const node = (id: string): Hint => ({
  branches: [],
  id: `call:${id}`,
  kind: "call",
  nodeId: id,
});
const manifest = (
  children: Hint[],
  nodes = ["analyse", "summarise"],
): Manifest => ({
  sdk: "ally.workflow",
  title: "测试工作流",
  description: "",
  edges: [],
  scripts: [],
  phases: [
    { title: "阶段", detail: "", id: "work" },
    { title: "阶段", detail: "", id: "result" },
  ],
  nodes: nodes.map((id) => ({
    execution: {
      codeDigest: "fixture",
      executionMode: "pure",
      language: "javascript",
    },
    inputs: [],
    kind: "functionTask",
    id,
    phaseId: "work",
    label: id,
  })),
  controls: children,
});

it("输入入口占据最左侧独立位置，不改变阶段顺序、节点身份或执行结构", () => {
  const source = manifest([node("analyse"), node("summarise")]);
  const before = workflowCanvasLayout(source);
  const after = workflowCanvasLayout(source, true);
  expect(
    after.map((phase) => ({
      ...phase,
      left: phase.left - workflowInputNodeWidth - 64,
    })),
  ).toEqual(before);
  expect(after[0]?.left).toBeGreaterThan(24 + workflowInputNodeWidth);
  expect(source.nodes).toHaveLength(2);
});

it("keeps a mapped parallel analysis as one task template followed by a sequential summary", () => {
  const phase = workflowCanvasLayout(
    manifest([
      {
        id: "batches",
        kind: "parallel",
        branches: [{ label: "items", children: [node("analyse")] }],
      },
      node("summarise"),
    ]),
  )[0];
  expect(phase?.items.map((item) => item.kind)).toEqual(["parallel", "node"]);
  expect(phase?.items[0]).toMatchObject({
    columns: 1,
    branches: [{ label: "items", items: [{ id: "analyse" }] }],
  });
  expect(phase?.items[1]).toMatchObject({ id: "summarise" });
});

it("lays independent tasks out beside each other and preserves the order inside each task", () => {
  const [phase, next] = workflowCanvasLayout(
    manifest(
      [
        {
          id: "tasks",
          kind: "parallel",
          branches: [
            { label: "1", children: [node("first"), node("second")] },
            { label: "2", children: [node("other")] },
          ],
        },
        node("summarise"),
      ],
      ["first", "second", "other", "summarise"],
    ),
  );
  expect(phase?.items[0]).toMatchObject({
    columns: 2,
    branches: [
      { items: [{ id: "first" }, { id: "second" }] },
      { items: [{ id: "other" }] },
    ],
  });
  expect(next?.left).toBe((phase?.left ?? 0) + (phase?.width ?? 0) + 64);
});

it("omits empty if/else lanes and keeps nested decisions in their own branch", () => {
  const phase = workflowCanvasLayout(
    manifest([
      {
        id: "check",
        kind: "decision",
        conditionExpression: "input.ok",
        branches: [
          {
            label: "true",
            children: [
              {
                id: "nested",
                kind: "decision",
                branches: [
                  { label: "true", children: [node("analyse")] },
                  { label: "false", children: [node("summarise")] },
                ],
              },
            ],
          },
          {
            label: "false",
            children: [{ branches: [], id: "early", kind: "sequence" }],
          },
        ],
      },
    ]),
  )[0];
  expect(phase?.items[0]).toMatchObject({
    columns: 2,
    branches: [{ items: [{ kind: "decision", columns: 2 }] }],
  });
});

it("keeps pipeline stages sequential and projects each phase without losing cross-phase branches", () => {
  const source = manifest([
    {
      id: "pipeline",
      kind: "pipeline",
      branches: [
        { label: "1", children: [node("analyse")] },
        { label: "2", children: [node("summarise")] },
      ],
    },
    {
      id: "decision",
      kind: "decision",
      branches: [
        { label: "true", children: [node("output")] },
        { label: "false", children: [] },
      ],
    },
  ]);
  source.nodes?.push({
    execution: {
      codeDigest: "fixture",
      executionMode: "pure",
      language: "javascript",
    },
    inputs: [],
    kind: "functionTask",
    label: "测试步骤",
    id: "output",
    phaseId: "result",
  });
  const phases = workflowCanvasLayout(source);
  expect(phases[0]?.items).toMatchObject([{ kind: "pipeline", columns: 1 }]);
  expect(phases[1]?.items).toMatchObject([
    {
      kind: "decision",
      columns: 1,
      branches: [{ items: [{ id: "output" }] }],
    },
  ]);
});

it("按行宽整体换行，并按本行最高阶段为下一行留出空间", () => {
  const source = manifest([]);
  source.phases = Array.from({ length: 5 }, (_, index) => ({
    id: `phase-${index}`,
    title: `阶段 ${index}`,
    detail: "",
  }));
  const layouts = workflowCanvasLayout(source, false, {
    maxWidth: 700,
    phaseHeights: { "phase-0": 500, "phase-1": 200 },
  });
  expect(layouts.map(({ left, top }) => [left, top])).toEqual([
    [24, 24],
    [368, 24],
    [24, 620],
    [368, 620],
    [24, 1052],
  ]);
});

it("宽容器仍限制每行最大宽度，输入入口参与第一行占位和高度", () => {
  const source = manifest([]);
  source.phases = Array.from({ length: 5 }, (_, index) => ({
    id: `phase-${index}`,
    title: `阶段 ${index}`,
    detail: "",
  }));
  const layouts = workflowCanvasLayout(source, true, {
    maxWidth: 2400,
    inputHeight: 600,
  });
  expect(layouts.map(({ left, top }) => [left, top])).toEqual([
    [368, 24],
    [712, 24],
    [1056, 24],
    [24, 720],
    [368, 720],
  ]);
});

it("过宽的并行阶段独占一行，保留其分支结构且不生成空白首行", () => {
  const source = manifest([
    {
      id: "parallel",
      kind: "parallel",
      branches: [
        { label: "1", children: [node("analyse")] },
        { label: "2", children: [node("summarise")] },
      ],
    },
  ]);
  const layouts = workflowCanvasLayout(source, false, { maxWidth: 320 });
  expect(layouts[0]).toMatchObject({ left: 24, top: 24, width: 552 });
  expect(layouts[0]?.items[0]).toMatchObject({ kind: "parallel", columns: 2 });
  expect(layouts[1]).toMatchObject({ left: 24, top: 456 });
});
