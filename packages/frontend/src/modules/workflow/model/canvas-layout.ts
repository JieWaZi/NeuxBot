import type { Manifest, WorkflowNode } from "./types";

type ControlHint = NonNullable<Manifest["controls"]>[number];
type GroupKind = "decision" | "parallel" | "pipeline" | "loop";
export type CanvasItem =
  | { id: string; kind: "node"; node: WorkflowNode; columns: number }
  | {
      id: string;
      kind: GroupKind;
      columns: number;
      branches: {
        id: string;
        label?: string;
        items: CanvasItem[];
        columns: number;
      }[];
    };

const columnCount = (items: CanvasItem[]) =>
  Math.max(1, ...items.map((item) => item.columns));

export const workflowInputNodeWidth = 280;

/** 只使用编译器提供的真实控制路径；节点标题与分组 ID 不用于猜测并发。 */
export function workflowCanvasLayout(
  manifest: Manifest,
  includeInput = false,
  {
    maxWidth = 1440,
    phaseHeights = {},
    inputHeight = 336,
  }: {
    maxWidth?: number;
    phaseHeights?: Record<string, number>;
    inputHeight?: number;
  } = {},
) {
  const rowWidth = Math.min(1440, Math.max(328, maxWidth));
  let left = 24 + (includeInput ? workflowInputNodeWidth + 64 : 0);
  let top = 24;
  let rowHeight = includeInput ? inputHeight : 0;
  return (manifest.phases ?? []).map((phase) => {
    const nodes = (manifest.nodes ?? [])
      .filter((node) => node.phaseId === phase.id)
      .sort(
        (a, b) =>
          (a.sourceRange?.start?.offset ?? 0) -
          (b.sourceRange?.start?.offset ?? 0),
      );
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const displayed = new Set<string>();
    const project = (hints: ControlHint[]): CanvasItem[] =>
      hints.flatMap((hint): CanvasItem[] => {
        if (hint.nodeId) {
          const node = byId.get(hint.nodeId);
          if (!node || displayed.has(hint.nodeId)) return [];
          displayed.add(hint.nodeId);
          return [{ id: hint.nodeId, kind: "node", node, columns: 1 }];
        }
        const branches = (hint.branches ?? [])
          .map((branch, index) => {
            const items = project(branch.children ?? []);
            return {
              id: `${hint.id}:${branch.label ?? index}`,
              label: branch.label,
              items,
              columns: columnCount(items),
            };
          })
          .filter((branch) => branch.items.length > 0);
        if (!branches.length) return [];
        const kind = hint.kind;
        if (
          kind !== "decision" &&
          kind !== "parallel" &&
          kind !== "pipeline" &&
          kind !== "loop"
        )
          return branches.flatMap((branch) => branch.items);
        const sideBySide = kind === "decision" || kind === "parallel";
        return [
          {
            id:
              hint.id ??
              `${kind}:${branches.map((branch) => branch.id).join(":")}`,
            kind,
            branches,
            columns: sideBySide
              ? branches.reduce((total, branch) => total + branch.columns, 0)
              : Math.max(1, ...branches.map((branch) => branch.columns)),
          },
        ];
      });
    const items = project(manifest.controls ?? []);
    // 未形成控制提示的调用仍保持编译器节点的源码顺序，不推断分组。
    for (const node of nodes)
      if (node.id && !displayed.has(node.id))
        items.push({ id: node.id, kind: "node", node, columns: 1 });
    const columns = columnCount(items);
    const width = 32 + columns * 248 + (columns - 1) * 24;
    // 阶段整体换行，过宽的并行阶段独占一行，保留真实分支结构。
    if (left > 24 && left + width + 24 > rowWidth) {
      top += rowHeight + 96;
      left = 24;
      rowHeight = 0;
    }
    const height = phaseHeights[phase.id] ?? 336;
    const layout = { phase, items, width, height, left, top };
    rowHeight = Math.max(rowHeight, height);
    left += width + 64;
    return layout;
  });
}

/** 将编译器节点依赖投影到阶段连线；阶段内部路径由 controls 表达。 */
export function workflowPhaseEdges(manifest: Manifest) {
  const nodes = new Map(
    (manifest.nodes ?? []).map((node) => [node.id, node.phaseId]),
  );
  const unique = new Map<string, { from: string; to: string }>();
  for (const edge of manifest.edges ?? []) {
    const from = nodes.get(edge.from);
    const to = nodes.get(edge.to);
    if (from && to && from !== to) unique.set(`${from}:${to}`, { from, to });
  }
  return [...unique.values()];
}

export function canvasControls(
  manifest?: Manifest,
): NonNullable<Manifest["controls"]> {
  const flatten = (
    controls: NonNullable<Manifest["controls"]>,
  ): NonNullable<Manifest["controls"]> =>
    controls.flatMap((control) => [
      control,
      ...flatten(
        control.branches?.flatMap((branch) => branch.children ?? []) ?? [],
      ),
    ]);
  return flatten(manifest?.controls ?? []);
}
