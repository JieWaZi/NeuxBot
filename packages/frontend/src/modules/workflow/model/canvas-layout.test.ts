import { expect, it } from "vitest";
import { workflowCanvasLayout, workflowInputNodeWidth } from "./canvas-layout";
import type { Manifest } from "./types";

type Hint = NonNullable<Manifest["controlHints"]>[number];
const node = (id: string, phaseId = "work"): Hint => ({
  id: `call:${id}`,
  kind: "node",
  nodeId: id,
  phaseId,
});
const manifest = (
  hints: Hint[],
  nodes = ["analyse", "summarise"],
): Manifest => ({
  meta: { phases: [{ id: "work" }, { id: "result" }] },
  nodes: nodes.map((id) => ({ id, phaseId: "work", labelTemplate: id })),
  controlHints: hints,
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
        branches: [{ label: "items", hints: [node("analyse")] }],
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
            { label: "1", hints: [node("first"), node("second")] },
            { label: "2", hints: [node("other")] },
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
        condition: "input.ok",
        branches: [
          {
            label: "true",
            hints: [
              {
                id: "nested",
                kind: "decision",
                branches: [
                  { label: "true", hints: [node("analyse")] },
                  { label: "false", hints: [node("summarise")] },
                ],
              },
            ],
          },
          {
            label: "false",
            hints: [{ id: "early", kind: "return", phaseId: "work" }],
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
        { label: "1", hints: [node("analyse")] },
        { label: "2", hints: [node("summarise")] },
      ],
    },
    {
      id: "decision",
      kind: "decision",
      branches: [
        { label: "true", hints: [node("output", "result")] },
        { label: "false", hints: [] },
      ],
    },
  ]);
  source.nodes?.push({ id: "output", phaseId: "result" });
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
