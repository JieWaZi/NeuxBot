import { render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import { UsageOverview, UsageReport } from "./usage-report";

it("shows a last-request Token subtotal and measured zero", () => {
  render(
    <UsageOverview
      summary={{
        coverage: { state: "partial", reasons: ["incomplete_tokens"] },
        metrics: {
          knownTotalWithCacheRead: 17,
          knownTotalWithoutCacheRead: 12,
          knownTokens: {
            input: 7,
            cacheRead: 5,
            cacheWrite: 0,
            output: 5,
            reasoning: 2,
          },
          invocations: 1,
          incompleteUsageInvocations: 1,
        },
      }}
    />,
  );

  expect(screen.getByText("17")).toBeInTheDocument();
  expect(screen.getByText("已记录 Token")).toBeInTheDocument();
  expect(screen.getByText("已记录推理输出").parentElement).toHaveTextContent(
    "2",
  );
  expect(screen.queryByText("费用")).not.toBeInTheDocument();
  expect(screen.getByText(/1 次调用的 Token 数据不完整/)).toBeInTheDocument();
  expect(screen.getByText("0")).toBeInTheDocument();
});

it("distinguishes identical model IDs from different providers", () => {
  const groups = [
    { key: "shared:model", provider: "claude", metrics: { invocations: 1 } },
    { key: "shared:model", provider: "cursor", metrics: { invocations: 2 } },
  ];
  render(
    <UsageReport
      groupBy="model"
      groups={groups}
      names={new Map()}
      more={false}
      loading={false}
      onLoadMore={() => {}}
    />,
  );

  expect(
    screen.getByRole("row", { name: /shared:model claude/ }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("row", { name: /shared:model cursor/ }),
  ).toBeInTheDocument();
});

it("uses the reported totals and shows the token categories without raw counters", () => {
  const summary = {
    metrics: {
      knownTotalWithCacheRead: 100_010_000,
      knownTotalWithoutCacheRead: 10_000,
      knownTokens: {
        reportedInput: 1200,
        cacheRead: 100_000_000,
        cacheWrite: 0,
        output: 9_999,
        reasoning: 8,
      },
      invocations: 1,
      incompleteUsageInvocations: 1,
    },
  };
  render(<UsageOverview summary={summary} />);
  expect(screen.getByText("100,010,000")).toBeInTheDocument();
  expect(screen.getByText("缓存输入").parentElement).toHaveTextContent("1.0亿");
  expect(screen.getByText("未缓存输入").parentElement).toHaveTextContent(
    "未知",
  );
  expect(screen.getByText("缓存写入").parentElement).toHaveTextContent("0");
  expect(screen.getByText("输出").parentElement).toHaveTextContent("9,999");
  expect(screen.queryByText("更多 Token 明细")).not.toBeInTheDocument();
  expect(screen.queryByText("1,200")).not.toBeInTheDocument();
});

it("keeps the reasoning column and distinguishes a measured zero from unknown", () => {
  const { rerender } = render(
    <UsageOverview
      summary={{
        metrics: { invocations: 1, tokens: { reasoning: 0 } },
      }}
    />,
  );
  expect(
    screen.getByText("推理输出").parentElement?.querySelector("dd"),
  ).toHaveTextContent(/^0$/);
  rerender(
    <UsageOverview
      summary={{ metrics: { invocations: 1, knownTokens: { output: 25 } } }}
    />,
  );
  expect(
    screen.getByText("推理输出").parentElement?.querySelector("dd"),
  ).toHaveTextContent("未知");
});

it("keeps Token counts unknown when an incomplete projection has no recorded calls", () => {
  render(
    <UsageOverview
      summary={{
        coverage: {
          state: "partial",
          reasons: ["native_projection_incomplete"],
        },
        metrics: { invocations: 0 },
      }}
    />,
  );
  expect(screen.getByText("未缓存输入").parentElement).toHaveTextContent(
    "未知",
  );
  expect(
    screen.getByText("推理输出").parentElement?.querySelector("dd"),
  ).toHaveTextContent("未知");
  expect(screen.queryByText("0 次调用")).not.toBeInTheDocument();
});

it("resolves native model names within their own runtime and retains unknown IDs", () => {
  render(
    <UsageReport
      groupBy="model"
      groups={[
        { provider: "qoder", key: "dfmodel", metrics: { invocations: 1 } },
        { provider: "workbuddy", key: "dfmodel", metrics: { invocations: 2 } },
        {
          provider: "qoder",
          key: "retired-model",
          metrics: { invocations: 3 },
        },
      ]}
      runtimes={[
        { id: "qoder", models: [{ id: "dfmodel", name: "DeepSeek-Flash" }] },
        { id: "workbuddy", models: [{ id: "dfmodel", name: "Other Model" }] },
      ]}
      names={new Map()}
      more={false}
      loading={false}
      onLoadMore={() => {}}
    />,
  );
  const row = screen.getByRole("row", { name: /DeepSeek-Flash qoder/ });
  expect(within(row).getByText("DeepSeek-Flash")).toBeInTheDocument();
  expect(
    screen.getByRole("row", { name: /Other Model workbuddy/ }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("row", { name: /retired-model qoder/ }),
  ).toBeInTheDocument();
});

it("keeps overview labels and its surface while only the values wait for another agent", () => {
  const { container, rerender } = render(
    <UsageOverview
      provider="claude"
      summary={{ metrics: { knownTotalWithCacheRead: 17, invocations: 2 } }}
    />,
  );
  const surface = container.firstElementChild;
  const inputLabel = screen.getByText("未缓存输入");

  rerender(<UsageOverview provider="codex" pending />);
  expect(screen.getByRole("status", { name: "用量统计" })).toBe(surface);
  expect(surface).toHaveAttribute("aria-busy", "true");
  expect(screen.getByText("未缓存输入")).toBe(inputLabel);
  expect(screen.getByText("Codex", { selector: "span" })).toBeInTheDocument();
  expect(screen.queryByText("17")).not.toBeInTheDocument();
  expect(screen.queryByText("2 次调用")).not.toBeInTheDocument();
  expect(screen.queryByText("未知")).not.toBeInTheDocument();

  rerender(
    <UsageOverview
      provider="codex"
      summary={{ metrics: { knownTotalWithCacheRead: 23, invocations: 3 } }}
    />,
  );
  expect(container.firstElementChild).toBe(surface);
  expect(screen.getByText("未缓存输入")).toBe(inputLabel);
  expect(screen.getByText("23")).toBeInTheDocument();
  expect(screen.getByText("3 次调用")).toBeInTheDocument();
  expect(surface).not.toHaveAttribute("aria-busy");
});

it("keeps the fifth metric slot while another agent's values load", () => {
  const { rerender } = render(
    <UsageOverview
      summary={{ metrics: { invocations: 1, tokens: { reasoning: 7 } } }}
    />,
  );
  const reasoningLabel = screen.getByText("推理输出");
  const slot = reasoningLabel.parentElement;
  const grid = slot?.parentElement;
  const columns = grid?.className;
  expect(slot?.querySelector("dd")).toHaveTextContent(/^7$/);
  expect(grid?.querySelectorAll("dt")).toHaveLength(5);

  rerender(<UsageOverview pending />);
  expect(screen.getByText("推理输出")).toBe(reasoningLabel);
  expect(grid?.className).toBe(columns);
  expect(grid?.querySelectorAll("dt")).toHaveLength(5);
  expect(slot?.querySelector("dd")).toHaveTextContent(/^$/);
  expect(slot?.querySelector("dd [aria-hidden='true']")).toBeInTheDocument();
  expect(screen.queryByText("7")).not.toBeInTheDocument();

  rerender(
    <UsageOverview
      summary={{ metrics: { invocations: 1, tokens: { reasoning: 11 } } }}
    />,
  );
  expect(screen.getByText("推理输出")).toBe(reasoningLabel);
  expect(slot?.querySelector("dd")).toHaveTextContent(/^11$/);

  rerender(<UsageOverview summary={{ metrics: { invocations: 1 } }} />);
  expect(screen.getByText("推理输出")).toBe(reasoningLabel);
  expect(slot?.querySelector("dd")).toHaveTextContent("未知");
  expect(grid?.querySelectorAll("dt")).toHaveLength(5);
});

it("keeps the visible table footprint as local row skeletons while a different scope loads", () => {
  const props = {
    names: new Map<string, string>(),
    loading: false,
    onLoadMore: () => {},
  };
  const { rerender } = render(
    <UsageReport
      {...props}
      groupBy="day"
      more
      groups={[
        {
          key: "first",
          metrics: { knownTotalWithCacheRead: 12, invocations: 1 },
        },
        { key: "second", metrics: { totalWithCacheRead: 17, invocations: 1 } },
        {
          key: "third",
          metrics: { knownTotalWithCacheRead: 24, invocations: 1 },
        },
      ]}
    />,
  );
  const body = screen.getByRole("table").querySelector("tbody");
  expect(body?.querySelectorAll("tr")).toHaveLength(3);
  rerender(
    <UsageReport {...props} groupBy="model" more={false} groups={[]} pending />,
  );
  expect(body?.querySelectorAll("tr")).toHaveLength(3);
  expect(
    body?.querySelectorAll(
      "tr:first-child td:nth-child(2) [aria-hidden='true']",
    ),
  ).toHaveLength(2);
  expect(
    body?.querySelectorAll(
      "tr:nth-child(2) td:nth-child(2) [aria-hidden='true']",
    ),
  ).toHaveLength(1);
  expect(screen.queryByText("first")).not.toBeInTheDocument();
  expect(screen.queryByText("12")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "加载更多" })).toBeDisabled();
});

it("keeps the table, columns and sections through loading, grouping and empty results", () => {
  const props = {
    names: new Map<string, string>(),
    more: false,
    loading: false,
    onLoadMore: () => {},
  };
  const { rerender } = render(
    <UsageReport
      {...props}
      groupBy="day"
      groups={[{ key: "2026-11-01", metrics: { invocations: 2 } }]}
    />,
  );
  const table = screen.getByRole("table");
  const head = table.querySelector("thead");
  const body = table.querySelector("tbody");
  const columns = table.querySelector("colgroup");

  rerender(<UsageReport {...props} groupBy="model" groups={[]} pending />);
  expect(screen.getByRole("status", { name: "用量明细" })).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(screen.getByRole("table")).toBe(table);
  expect(table.querySelector("thead")).toBe(head);
  expect(table.querySelector("tbody")).toBe(body);
  expect(table.querySelector("colgroup")).toBe(columns);
  expect(
    screen.getByRole("columnheader", { name: "模型" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("rowheader")).not.toBeInTheDocument();

  rerender(
    <UsageReport
      {...props}
      groupBy="model"
      groups={[]}
      emptyLabel="这个范围没有已记录的调用。"
    />,
  );
  expect(screen.getByRole("table")).toBe(table);
  expect(table.querySelector("thead")).toBe(head);
  expect(table.querySelector("tbody")).toBe(body);
  expect(
    within(table).getByText("这个范围没有已记录的调用。"),
  ).toBeInTheDocument();

  rerender(
    <UsageReport
      {...props}
      groupBy="model"
      groups={[{ key: "new-model", metrics: { invocations: 1 } }]}
    />,
  );
  expect(screen.getByRole("table")).toBe(table);
  expect(table.querySelector("thead")).toBe(head);
  expect(table.querySelector("tbody")).toBe(body);
  expect(
    screen.getByRole("rowheader", { name: "new-model" }),
  ).toBeInTheDocument();
});
