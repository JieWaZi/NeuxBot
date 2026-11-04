import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { UsageOverview, UsageReport } from "./usage-report";

it("shows a last-request subtotal without presenting unknown cost as zero", () => {
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
          unpricedInvocations: 1,
          amounts: [],
        },
      }}
    />,
  );

  expect(screen.getByText("17")).toBeInTheDocument();
  expect(screen.getByText("已知小计")).toBeInTheDocument();
  expect(screen.getByText("未定价")).toBeInTheDocument();
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
      metric="tokens"
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
