import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { UsageProviderBreakdown } from "./usage-provider-breakdown";

it("keeps the provider list footprint while only its rows wait for a new scope", () => {
  const { rerender } = render(
    <UsageProviderBreakdown
      total={200}
      groups={[
        { key: "codex", metrics: { totalWithCacheRead: 150 } },
        { key: "claude", metrics: { totalWithCacheRead: 50 } },
      ]}
    />,
  );
  const list = screen.getByRole("list", { name: "智能体用量" });
  expect(list.querySelectorAll("li")).toHaveLength(2);
  rerender(<UsageProviderBreakdown total={undefined} groups={[]} pending />);
  expect(screen.getByRole("status", { name: "智能体用量" })).toContainElement(
    list,
  );
  expect(list.querySelectorAll("li")).toHaveLength(2);
  expect(
    list.querySelector("li")?.querySelectorAll("[aria-hidden='true']"),
  ).toHaveLength(3);
  expect(screen.queryByText("150")).not.toBeInTheDocument();
  expect(list).not.toHaveTextContent("Codex");
  rerender(
    <UsageProviderBreakdown total={0} groups={[]} emptyLabel="没有调用记录" />,
  );
  expect(screen.getByRole("list", { name: "智能体用量" })).toBe(list);
  expect(list).toHaveTextContent("没有调用记录");
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});
