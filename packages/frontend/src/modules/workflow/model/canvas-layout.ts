import type { Manifest, WorkflowNode } from "./types";

type ControlHint = NonNullable<Manifest["controlHints"]>[number];
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
export function workflowCanvasLayout(manifest: Manifest, includeInput = false) {
  let left = 24 + (includeInput ? workflowInputNodeWidth + 64 : 0);
  return (manifest.meta?.phases ?? []).map((phase) => {
    const nodes = (manifest.nodes ?? [])
      .filter((node) => node.phaseId === phase.id)
      .sort(
        (a, b) => (a.range?.start?.offset ?? 0) - (b.range?.start?.offset ?? 0),
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
            const items = project(branch.hints ?? []);
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
            id: hint.id ?? `${kind}:${hint.range?.start?.offset ?? 0}`,
            kind,
            branches,
            columns: sideBySide
              ? branches.reduce((total, branch) => total + branch.columns, 0)
              : Math.max(1, ...branches.map((branch) => branch.columns)),
          },
        ];
      });
    const items = project(manifest.controlHints ?? []);
    // 未形成控制提示的调用仍保持编译器节点的源码顺序，不推断分组。
    for (const node of nodes)
      if (node.id && !displayed.has(node.id))
        items.push({ id: node.id, kind: "node", node, columns: 1 });
    const columns = columnCount(items);
    const width = 32 + columns * 248 + (columns - 1) * 24;
    const layout = { phase, items, width, left };
    left += width + 64;
    return layout;
  });
}
