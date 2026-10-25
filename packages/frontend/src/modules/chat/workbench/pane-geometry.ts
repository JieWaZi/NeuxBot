import type {
  WorkbenchNode,
  WorkbenchPane,
  WorkbenchSplit,
} from "./workbench-pane";

export type PaneRect = {
  left: number;
  top: number;
  width: number;
  height: number;
};

export type PaneGeometry = {
  panes: Array<{ pane: WorkbenchPane; rect: PaneRect }>;
  splits: Array<{ split: WorkbenchSplit; rect: PaneRect }>;
};

export type PaneHeaderGeometry = Array<{
  pane: WorkbenchPane;
  left: number;
  width: number;
}>;

/** 顶栏只承载贴着画布顶部的 pane；下方 pane 在自己的内容区域显示标签。 */
export function paneHeaderGeometry(node: WorkbenchNode): PaneHeaderGeometry {
  return paneGeometry(node).panes.flatMap(({ pane, rect }) =>
    rect.top === 0 ? [{ pane, left: rect.left, width: rect.width }] : [],
  );
}

/** 内容始终按标签身份挂在根层，pane 几何只决定位置。 */
export function paneGeometry(node: WorkbenchNode): PaneGeometry {
  const geometry: PaneGeometry = { panes: [], splits: [] };
  const visit = (current: WorkbenchNode, rect: PaneRect) => {
    if (current.kind === "pane") {
      geometry.panes.push({ pane: current, rect });
      return;
    }
    geometry.splits.push({ split: current, rect });
    const ratio = current.ratio;
    if (current.direction === "row") {
      const firstWidth = rect.width * ratio;
      visit(current.first, { ...rect, width: firstWidth });
      visit(current.second, {
        ...rect,
        left: rect.left + firstWidth,
        width: rect.width - firstWidth,
      });
      return;
    }
    const firstHeight = rect.height * ratio;
    visit(current.first, { ...rect, height: firstHeight });
    visit(current.second, {
      ...rect,
      top: rect.top + firstHeight,
      height: rect.height - firstHeight,
    });
  };
  visit(node, { left: 0, top: 0, width: 100, height: 100 });
  return geometry;
}
