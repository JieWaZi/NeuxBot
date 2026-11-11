import { render, screen } from "@testing-library/react";
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
  expect(screen.queryByText("已知小计")).not.toBeInTheDocument();
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
      totalGroups={2}
      more={false}
      loading={false}
      onLoadMore={() => {}}
    />,
  );

  expect(screen.getByText("claude / shared:model")).toBeInTheDocument();
  expect(screen.getByText("cursor / shared:model")).toBeInTheDocument();
});

it("keeps reported input visible without adding it to the known Token subtotal", () => {
  const summary = {
    metrics: {
      knownTotalWithCacheRead: 520,
      knownTokens: { reportedInput: 1200, cacheRead: 500, output: 20 },
      invocations: 1,
      incompleteUsageInvocations: 1,
    },
  };
  render(<UsageOverview summary={summary} />);
  expect(screen.getByText("原生输入")).toBeInTheDocument();
  expect(screen.getByText("1,200")).toBeInTheDocument();
  expect(screen.getByText("520")).toBeInTheDocument();
  expect(screen.queryByText("1,720")).not.toBeInTheDocument();
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
      totalGroups={3}
      more={false}
      loading={false}
      onLoadMore={() => {}}
    />,
  );
  expect(screen.getByText("qoder / DeepSeek-Flash")).toBeInTheDocument();
  expect(screen.getByText("workbuddy / Other Model")).toBeInTheDocument();
  expect(screen.getByText("qoder / retired-model")).toBeInTheDocument();
});
