import { describe, expect, it } from "vitest";

import { paneGeometry, paneHeaderGeometry } from "./pane-geometry";

describe("paneGeometry", () => {
  it("计算递归横纵分割的稳定百分比矩形", () => {
    const result = paneGeometry({
      kind: "split",
      id: "root",
      direction: "row",
      ratio: 0.4,
      first: { kind: "pane", id: "a", tabIds: ["a"] },
      second: {
        kind: "split",
        id: "nested",
        direction: "column",
        ratio: 0.25,
        first: { kind: "pane", id: "b", tabIds: ["b"] },
        second: { kind: "pane", id: "c", tabIds: ["c"] },
      },
    });
    expect(result.panes.map(({ pane, rect }) => [pane.id, rect])).toEqual([
      ["a", { left: 0, top: 0, width: 40, height: 100 }],
      ["b", { left: 40, top: 0, width: 60, height: 25 }],
      ["c", { left: 40, top: 25, width: 60, height: 75 }],
    ]);
    expect(result.splits.map(({ split }) => split.id)).toEqual([
      "root",
      "nested",
    ]);
  });
});

describe("paneHeaderGeometry", () => {
  it("顶栏只保留贴着顶部的 pane，宽度与内容对齐", () => {
    const result = paneHeaderGeometry({
      kind: "split",
      id: "root",
      direction: "row",
      ratio: 0.4,
      first: { kind: "pane", id: "a", tabIds: ["a"] },
      second: {
        kind: "split",
        id: "nested",
        direction: "column",
        ratio: 0.25,
        first: { kind: "pane", id: "b", tabIds: ["b"] },
        second: { kind: "pane", id: "c", tabIds: ["c"] },
      },
    });
    expect(
      result.map(({ pane, left, width }) => [pane.id, left, width]),
    ).toEqual([
      ["a", 0, 40],
      ["b", 40, 60],
    ]);
  });

  it("纵向分屏的下方 pane 不占据顶栏", () => {
    const result = paneHeaderGeometry({
      kind: "split",
      id: "root",
      direction: "column",
      ratio: 0.2,
      first: { kind: "pane", id: "a", tabIds: ["a"] },
      second: { kind: "pane", id: "b", tabIds: ["b"] },
    });
    expect(
      result.map(({ pane, left, width }) => [pane.id, left, width]),
    ).toEqual([["a", 0, 100]]);
  });
});
