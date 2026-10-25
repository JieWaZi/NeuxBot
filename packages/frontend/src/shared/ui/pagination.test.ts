import { describe, expect, it } from "vitest";

import { paginationItems } from "./pagination";

describe("paginationItems", () => {
  it("完整展示较少页码", () => {
    expect(paginationItems(2, 4)).toEqual([1, 2, 3, 4]);
  });

  it("在首页附近省略尾部页码", () => {
    expect(paginationItems(3, 12)).toEqual([1, 2, 3, 4, 5, "end-gap", 12]);
  });

  it("在中间页保留两侧边界", () => {
    expect(paginationItems(6, 12)).toEqual([
      1,
      "start-gap",
      5,
      6,
      7,
      "end-gap",
      12,
    ]);
  });

  it("在尾页附近省略头部页码", () => {
    expect(paginationItems(11, 12)).toEqual([1, "start-gap", 8, 9, 10, 11, 12]);
  });
});
