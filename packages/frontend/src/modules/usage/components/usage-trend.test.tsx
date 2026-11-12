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
  expect(last).toHaveFocus();
  expect(await screen.findByRole("tooltip")).toHaveTextContent(
    "11月12日 · 已记录 1.5万 Token",
  );
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
});

it("plots a zero for an idle retained day and leaves earlier days blank", () => {
  const { container } = render(
    <UsageTrend
      from="2026-11-09"
      to="2026-11-12"
      earliestRetainedDay="2026-11-10"
      hasMore={false}
      groups={[
        { key: "2026-11-10", metrics: { knownTokens: { input: 12 } } },
        { key: "2026-11-12", metrics: { knownTokens: { input: 4 } } },
      ]}
    />,
  );
  const curve = container.querySelector("svg path[stroke='currentColor']");
  expect(curve?.getAttribute("d")).toMatch(/^M 375 /);
  expect(curve?.getAttribute("d")).toMatch(/ 625 280 C /);
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
