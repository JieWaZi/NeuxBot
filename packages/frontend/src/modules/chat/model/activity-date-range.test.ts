import { describe, expect, it } from "vitest";

import {
  activityDateRange,
  chatPageSizeFromSearchParam,
  localDateFromInput,
  localDateToInput,
  normalizeDateInput,
  pageFromSearchParam,
} from "./activity-date-range";

describe("activity date range", () => {
  it("按本地自然日生成包含边界的时间范围", () => {
    const range = activityDateRange("2026-08-01", "2026-08-02");
    const start = new Date(range.updatedAtStart ?? 0);
    const end = new Date(range.updatedAtEnd ?? 0);

    expect(range.valid).toBe(true);
    expect([
      start.getFullYear(),
      start.getMonth() + 1,
      start.getDate(),
      start.getHours(),
      start.getMinutes(),
    ]).toEqual([2026, 8, 1, 0, 0]);
    expect([
      end.getFullYear(),
      end.getMonth() + 1,
      end.getDate(),
      end.getHours(),
      end.getMinutes(),
      end.getSeconds(),
      end.getMilliseconds(),
    ]).toEqual([2026, 8, 2, 23, 59, 59, 999]);
  });

  it("允许单边范围并拒绝倒序范围", () => {
    expect(activityDateRange("2026-08-01", "").valid).toBe(true);
    expect(activityDateRange("", "2026-08-02").valid).toBe(true);
    expect(activityDateRange("2026-08-03", "2026-08-02").valid).toBe(false);
  });

  it("清理非法日期和页码", () => {
    expect(normalizeDateInput("2026-02-30")).toBe("");
    expect(pageFromSearchParam("3")).toBe(3);
    expect(pageFromSearchParam("0")).toBe(1);
    expect(pageFromSearchParam("2.5")).toBe(1);
    expect(chatPageSizeFromSearchParam(null)).toBe(10);
    expect(chatPageSizeFromSearchParam("30")).toBe(30);
    expect(chatPageSizeFromSearchParam("50")).toBe(50);
    expect(chatPageSizeFromSearchParam("100")).toBe(10);
  });

  it("在日期选择器与 URL 日期之间双向转换", () => {
    const date = localDateFromInput("2026-08-05");
    expect(date).toBeDefined();
    expect(localDateToInput(date)).toBe("2026-08-05");
  });
});
