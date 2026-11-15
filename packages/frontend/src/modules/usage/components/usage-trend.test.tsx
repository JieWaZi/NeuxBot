import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { UsageTrend } from "./usage-trend";

it("opens a Token tooltip on hover and supports moving between recorded dates by keyboard", async () => {
  const user = userEvent.setup();
  render(
    <UsageTrend
      from="2026-11-10"
      to="2026-11-12"
      hasMore={false}
      groups={[
        {
          key: "2026-11-10",
          metrics: {
            totalWithCacheRead: 4_677_000,
            knownTokens: { input: 1_000, cacheRead: 4_676_000, output: 0 },
          },
        },
        { key: "2026-11-12", metrics: { knownTotalWithCacheRead: 15_000 } },
      ]}
    />,
  );

  const first = screen.getByRole("button", {
    name: "11月10日 · 467.7万 Token",
  });
  const last = screen.getByRole("button", {
    name: "11月12日 · 已记录 1.5万 Token",
  });
  const idle = screen.getByRole("button", { name: "11月11日 · 0 Token" });
  await user.hover(first);
  expect(await screen.findByRole("tooltip")).toHaveTextContent(
    "11月10日 · 467.7万 Token",
  );
  expect(screen.getByRole("tooltip")).toHaveTextContent(/缓存输入\s*467\.6万/);
  expect(screen.getByRole("tooltip")).toHaveTextContent(/输出\s*0/);
  expect(screen.getByRole("tooltip")).toHaveTextContent(/缓存写入\s*未知/);

  await user.unhover(first);
  await user.tab();
  expect(first).toHaveFocus();
  await user.keyboard("{ArrowRight}");
  expect(idle).toHaveFocus();
  expect(await screen.findByRole("tooltip")).toHaveTextContent(
    "11月11日 · 0 Token",
  );
  await user.keyboard("{ArrowRight}");
  expect(last).toHaveFocus();
  expect(await screen.findByRole("tooltip")).toHaveTextContent(
    "11月12日 · 已记录 1.5万 Token",
  );
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
});

it("connects idle days to the first recorded call from the start of the selected range", () => {
  const { container } = render(
    <UsageTrend
      from="2026-11-09"
      to="2026-11-12"
      hasMore={false}
      groups={[
        { key: "2026-11-10", metrics: { knownTokens: { input: 12 } } },
        { key: "2026-11-12", metrics: { knownTokens: { input: 4 } } },
      ]}
    />,
  );
  const curve = container.querySelector(
    "svg g.text-status-info-content path[stroke='currentColor']",
  );
  expect(curve?.getAttribute("d")).toMatch(/^M 125 280 C /);
  expect(curve?.getAttribute("d")).toMatch(/ 625 280 C /);
  expect(
    screen.getByRole("button", { name: "11月9日 · 0 Token" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "11月11日 · 0 Token" }),
  ).toBeInTheDocument();
});

it("keeps a missing Token category unknown on a recorded day", () => {
  const { container } = render(
    <UsageTrend
      from="2026-11-10"
      to="2026-11-12"
      hasMore={false}
      groups={[
        { key: "2026-11-10", metrics: { knownTokens: { input: 12 } } },
        { key: "2026-11-11", metrics: { knownTokens: { output: 6 } } },
        { key: "2026-11-12", metrics: { knownTokens: { input: 4 } } },
      ]}
    />,
  );
  const inputSegments = container.querySelectorAll(
    "svg g.text-status-info-content path[stroke='currentColor']",
  );
  expect(inputSegments).toHaveLength(2);
  expect(
    screen.getByRole("button", { name: "11月11日 · 未知" }),
  ).toBeInTheDocument();
});

it("keeps the chart mounted through pending data and draws zero lines for a confirmed empty range", () => {
  const { container, rerender } = render(
    <UsageTrend
      from="2026-11-10"
      to="2026-11-12"
      hasMore={false}
      groups={[]}
      pending
    />,
  );
  const figure = screen.getByRole("status", { name: "用量趋势" });
  const plot = screen.getByRole("group", { name: "用量趋势" });
  const svg = container.querySelector("svg");
  expect(figure).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(svg?.querySelector("path")).toBeNull();

  rerender(
    <UsageTrend
      from="2026-11-10"
      to="2026-11-12"
      hasMore={false}
      groups={[]}
    />,
  );
  expect(container.querySelector("figure")).toBe(figure);
  expect(screen.getByRole("group", { name: "用量趋势" })).toBe(plot);
  expect(container.querySelector("svg")).toBe(svg);
  expect(svg?.querySelectorAll("path[stroke='currentColor']")).toHaveLength(4);
  for (const line of svg?.querySelectorAll("path[stroke='currentColor']") ??
    []) {
    expect(line.getAttribute("d")).toMatch(/^M \S+ 280 C .+ 280$/);
  }
  expect(screen.getAllByRole("button")).toHaveLength(3);
  expect(
    screen.getByRole("button", { name: "11月10日 · 0 Token" }),
  ).toBeInTheDocument();
});

it("does not fill not-yet-loaded days beyond the current page", () => {
  render(
    <UsageTrend
      from="2026-11-09"
      to="2026-11-12"
      hasMore
      groups={[{ key: "2026-11-10", metrics: { knownTokens: { input: 12 } } }]}
    />,
  );
  expect(screen.getAllByRole("button")).toHaveLength(2);
  expect(
    screen.getByRole("button", { name: "11月9日 · 0 Token" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /11月11日/ }),
  ).not.toBeInTheDocument();
});

it("leaves the trend blank when no days are available in an incomplete projection", () => {
  render(
    <UsageTrend
      from="2026-11-10"
      to="2026-11-12"
      hasMore={false}
      projectionPartial
      groups={[]}
    />,
  );
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
