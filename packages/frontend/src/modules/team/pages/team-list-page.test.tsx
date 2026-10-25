import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { TeamCollection } from "./team-list-page";

const fixture = vi.hoisted(() => ({
  load: vi.fn(),
  pending: false,
}));

vi.mock("../api/channel-queries", () => ({
  listChannelsOptions: ({ query }: { query: Record<string, unknown> }) => ({
    queryKey: ["channels", query],
    queryFn: async () => {
      fixture.load(query);
      if (fixture.pending) return new Promise(() => undefined);
      return {
        resources: [{ channel: { id: "team-1", name: "研发团队" } }],
        totalSize: 25,
      };
    },
  }),
}));

vi.mock("../components/team-card", () => ({
  TeamCard: ({ detail }: { detail: { channel?: { name?: string } } }) => (
    <article>{detail.channel?.name}</article>
  ),
}));

vi.mock("../components/team-form-dialog", () => ({
  TeamFormDialog: () => null,
}));

function LocationValue() {
  const location = useLocation();
  return <output aria-label="location">{location.search}</output>;
}

function renderCollection(initialEntry = "/teams") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <TeamCollection />
        <LocationValue />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("TeamCollection", () => {
  beforeEach(() => {
    fixture.load.mockClear();
    fixture.pending = false;
  });

  it("初次加载展示骨架屏", () => {
    fixture.pending = true;
    renderCollection();

    expect(screen.getAllByRole("status")).not.toHaveLength(0);
    expect(screen.queryByText("研发团队")).not.toBeInTheDocument();
  });

  it("从 URL 恢复页码且不发送状态筛选", async () => {
    renderCollection("/teams?teamPage=2&keep=yes");

    expect(await screen.findByText("研发团队")).toBeVisible();
    expect(fixture.load).toHaveBeenCalledWith({
      pageNum: 2,
      pageSize: 12,
    });
    expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  });

  it("使用与伙伴页一致的工具栏布局并支持搜索团队", async () => {
    const user = userEvent.setup();
    renderCollection();

    expect(
      screen.getByRole("searchbox", { name: "搜索团队" }).parentElement,
    ).toHaveClass(
      "basis-field-search-min",
      "min-w-field-search-min",
      "max-w-field-search",
      "grow-[4]",
    );
    expect(screen.getByText("全部团队")).toBeVisible();
    expect(await screen.findByText("25 个团队")).toBeVisible();

    await user.type(
      screen.getByRole("searchbox", { name: "搜索团队" }),
      "研发",
    );
    expect(await screen.findByText("研发团队")).toBeVisible();
    expect(fixture.load).toHaveBeenLastCalledWith({
      pageNum: 1,
      pageSize: 12,
      query: "研发",
    });
    expect(screen.getByLabelText("location")).toHaveTextContent(
      "?q=%E7%A0%94%E5%8F%91",
    );
  });
});
