export type WorkbenchPane = {
  kind: "pane";
  id: string;
  tabIds: string[];
  activeTabId?: string;
};

export type WorkbenchSplit = {
  kind: "split";
  id: string;
  direction: "row" | "column";
  ratio: number;
  first: WorkbenchNode;
  second: WorkbenchNode;
};

export type WorkbenchNode = WorkbenchPane | WorkbenchSplit;
export type WorkbenchDropZone = "left" | "right" | "up" | "down" | "center";

export function workbenchPanes(node: WorkbenchNode): WorkbenchPane[] {
  return node.kind === "pane"
    ? [node]
    : [...workbenchPanes(node.first), ...workbenchPanes(node.second)];
}

export function workbenchPaneById(
  node: WorkbenchNode,
  paneId: string,
): WorkbenchPane | undefined {
  return workbenchPanes(node).find((pane) => pane.id === paneId);
}

export function workbenchPaneWithTab(
  node: WorkbenchNode,
  tabId: string,
): WorkbenchPane | undefined {
  return workbenchPanes(node).find((pane) => pane.tabIds.includes(tabId));
}

export function adjacentWorkbenchPane(
  node: WorkbenchNode,
  paneId: string,
): WorkbenchPane | undefined {
  if (node.kind === "pane") return undefined;
  if (workbenchPaneById(node.first, paneId))
    return (
      adjacentWorkbenchPane(node.first, paneId) ??
      workbenchPanes(node.second)[0]
    );
  if (workbenchPaneById(node.second, paneId))
    return (
      adjacentWorkbenchPane(node.second, paneId) ??
      workbenchPanes(node.first).at(-1)
    );
  return undefined;
}

export function mapWorkbenchPane(
  node: WorkbenchNode,
  paneId: string,
  update: (pane: WorkbenchPane) => WorkbenchNode,
): WorkbenchNode {
  if (node.kind === "pane") return node.id === paneId ? update(node) : node;
  const first = mapWorkbenchPane(node.first, paneId, update);
  const second = mapWorkbenchPane(node.second, paneId, update);
  return first === node.first && second === node.second
    ? node
    : { ...node, first, second };
}

export function removeWorkbenchPane(
  node: WorkbenchNode,
  paneId: string,
): WorkbenchNode | undefined {
  if (node.kind === "pane") return node.id === paneId ? undefined : node;
  const first = removeWorkbenchPane(node.first, paneId);
  const second = removeWorkbenchPane(node.second, paneId);
  if (!first) return second;
  if (!second) return first;
  return first === node.first && second === node.second
    ? node
    : { ...node, first, second };
}

export function mapWorkbenchSplit(
  node: WorkbenchNode,
  splitId: string,
  update: (split: WorkbenchSplit) => WorkbenchSplit,
): WorkbenchNode {
  if (node.kind === "pane") return node;
  if (node.id === splitId) return update(node);
  const first = mapWorkbenchSplit(node.first, splitId, update);
  const second = mapWorkbenchSplit(node.second, splitId, update);
  return first === node.first && second === node.second
    ? node
    : { ...node, first, second };
}

export function clampWorkbenchRatio(ratio: number): number {
  return Math.max(0.08, Math.min(0.92, ratio));
}
