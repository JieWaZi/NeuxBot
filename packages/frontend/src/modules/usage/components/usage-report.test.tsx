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

it("shows an explicitly measured reasoning zero and hides missing reasoning", () => {
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
  expect(screen.queryByText(/推理输出/)).not.toBeInTheDocument();
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
  expect(screen.queryByText(/推理输出/)).not.toBeInTheDocument();
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
