import { expect, it } from "vitest";
import { usageGroupsOptions, usageSummaryOptions } from "../api/usage-queries";
import {
  parseUsageFilters,
  rangeFromUsageDates,
  usageFiltersSearch,
} from "./usage-filters";

it("retains range, time zone, and normalized filters in the URL", () => {
  const range = rangeFromUsageDates(
    "2026-03-08",
    "2026-03-08",
    "America/New_York",
  );
  expect(range).toEqual({
    from: Date.parse("2026-03-08T05:00:00Z"),
    to: Date.parse("2026-03-09T04:00:00Z"),
  });
  const search = new URLSearchParams({
    from: String(range?.from),
    to: String(range?.to),
    timezone: "America/New_York",
    groupBy: "model",
    provider: "codex",
    source: "workflow",
    metric: "cost",
  });
  search.append("waker", "z");
  search.append("waker", "a");
  search.append("waker", "z");
  const filters = parseUsageFilters(search);
  expect(filters.wakerIds).toEqual(["a", "z"]);
  const normalized = usageFiltersSearch(filters);
  expect(normalized.has("metric")).toBe(false);
  expect(normalized.get("provider")).toBe("codex");
  expect(normalized.get("source")).toBe("workflow");
  expect(parseUsageFilters(normalized)).toEqual(filters);
  expect(usageSummaryOptions(filters).queryKey[0]?.query).toMatchObject({
    provider: "codex",
    source: "workflow",
  });
  expect(
    usageGroupsOptions(filters, "snapshot").queryKey[0]?.query,
  ).toMatchObject({
    provider: "codex",
    source: "workflow",
    snapshot: "snapshot",
  });
});

it("defaults to thirty local calendar days, including the current date", () => {
  const filters = parseUsageFilters(
    new URLSearchParams({ timezone: "America/New_York" }),
    new Date("2026-03-09T17:00:00Z"),
  );
  expect(filters.from).toBe(Date.parse("2026-02-08T05:00:00Z"));
  expect(filters.to).toBe(Date.parse("2026-03-10T04:00:00Z"));
});
