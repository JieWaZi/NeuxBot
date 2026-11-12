import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";

import { UsagePage } from "./usage-page";

const fixture = vi.hoisted(() => ({
  snapshot: "old",
  empty: false,
  groups:
    vi.fn<
      (
        snapshot: string,
        cursor: string,
        signal: AbortSignal,
        groupBy: string,
      ) => Promise<unknown>
    >(),
}));

vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
vi.mock("@/shared/api/runtime-queries", () => ({
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => ({
      resources: [
        { id: "qoder", models: [{ id: "dfmodel", name: "DeepSeek-Flash" }] },
        { id: "kimi" },
      ],
    }),
  }),
}));

vi.mock("../api/usage-queries", () => ({
  usageSummaryOptions: () => ({
    queryKey: ["usage-summary"],
    queryFn: async () => ({
      snapshot: fixture.snapshot,
      metrics: fixture.empty
        ? { invocations: 0 }
        : {
            invocations: fixture.snapshot === "old" ? 11 : 22,
            knownTotalWithCacheRead: 200,
          },
    }),
  }),
  usageGroupsOptions: ({ groupBy }: { groupBy: string }, snapshot: string) => ({
    queryKey: ["usage-groups", snapshot, groupBy],
    queryFn: ({
      signal,
      pageParam,
    }: {
      signal: AbortSignal;
      pageParam: string;
    }) => fixture.groups(snapshot, pageParam, signal, groupBy),
  }),
}));

const page = (snapshot: string, key: string, nextCursor?: string) => ({
  snapshot,
  metrics: {
    invocations: snapshot === "old" ? 11 : 22,
    knownTotalWithCacheRead: 200,
  },
  groups: [{ key, metrics: { invocations: 1 } }],
  totalGroups: nextCursor ? 2 : 1,
  hasMore: Boolean(nextCursor),
  nextCursor,
});

function deferred() {
  let resolve: (value: unknown) => void = () => {};
  const promise = new Promise<unknown>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

const novemberRange = `timezone=UTC&from=${Date.parse("2026-11-01")}&to=${Date.parse("2026-11-07")}`;

function openPage(
  search = novemberRange,
  onSearchChange: (search: URLSearchParams) => void = () => {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  });
  const page = () => (
    <QueryClientProvider client={client}>
      <UsagePage
        search={new URLSearchParams(search)}
        onSearchChange={onSearchChange}
        wakerOptions={[{ value: "partner", label: "测试伙伴" }]}
      />
    </QueryClientProvider>
  );
  render(page());
  return async () => {
    fixture.snapshot = "new";
    await act(async () => {
      await client.invalidateQueries({
        queryKey: ["usage-summary"],
        exact: true,
      });
    });
  };
}

beforeEach(() => {
  fixture.snapshot = "old";
  fixture.empty = false;
  fixture.groups.mockReset();
});

it("shows zero counters and a zero trend when the selected range has no invocations", async () => {
  fixture.empty = true;
  fixture.groups.mockImplementation(async (snapshot) => ({
    ...page(snapshot, ""),
    groups: [],
  }));
  openPage(`${novemberRange}&provider=grok&groupBy=model`);
  expect(await screen.findByText("0 次调用")).toBeInTheDocument();
  expect(
    screen.getByText("缓存写入", { selector: "dt" }).parentElement,
  ).toHaveTextContent("0");
  expect(screen.getByRole("meter", { name: "缓存命中率" })).toHaveAttribute(
    "aria-valuetext",
    "0%",
  );
  const trend = screen.getByRole("group", { name: "用量趋势" });
  expect(
    within(trend).getByRole("button", { name: "11月1日 · 0 Token" }),
  ).toBeInTheDocument();
});

it("shows six primary agents and selects another agent from the overflow menu", async () => {
  const user = userEvent.setup();
  fixture.groups.mockImplementation(async (snapshot, _, __, groupBy) =>
    groupBy === "provider"
      ? { ...page(snapshot, ""), groups: [] }
      : page(snapshot, "2026-11-01"),
  );
  const navigate = vi.fn<(search: URLSearchParams) => void>();
  openPage(`${novemberRange}&source=workflow&provider=qoder`, navigate);
  expect(
    screen.getByRole("combobox", { name: "筛选调用来源" }),
  ).toHaveTextContent("工作流");
  const agents = screen.getByRole("radiogroup", { name: "筛选智能体" });
  expect(
    within(agents)
      .getAllByRole("radio")
      .map((radio) => radio.getAttribute("aria-label")),
  ).toEqual([
    "全部智能体",
    "Claude",
    "Codex",
    "Cursor",
    "Grok Build",
    "OpenCode",
    "Pi",
  ]);
  const more = screen.getByRole("button", { name: /其他智能体.*Qoder/ });
  const track = agents.closest(".segmented-control-shell");
  expect(track).toContainElement(more);
  expect(
    within(agents).queryByRole("button", { name: /其他智能体/ }),
  ).toBeNull();
  more.focus();
  await user.keyboard("{Enter}");
  expect(
    await screen.findByRole("menuitemradio", { name: "Qoder", checked: true }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("menuitemradio", { name: "Kimi" }));
  const search = navigate.mock.calls[0]?.[0];
  expect(search?.get("provider")).toBe("kimi");
  expect(search?.get("source")).toBe("workflow");
  expect(search?.get("from")).toBe(String(Date.parse("2026-11-01")));
});

it("offers only the four supported source filters and treats an old source link as all sources", async () => {
  const user = userEvent.setup();
  fixture.groups.mockImplementation(async (snapshot) =>
    page(snapshot, "2026-11-01"),
  );
  openPage(`${novemberRange}&source=knowledge_generation`);
  const source = screen.getByRole("combobox", { name: "筛选调用来源" });
  expect(source).toHaveTextContent("全部来源");
  source.focus();
  await user.keyboard("{Enter}");
  const options = within(await screen.findByRole("listbox")).getAllByRole(
    "option",
  );
  expect(options.map((option) => option.textContent)).toEqual([
    "全部来源",
    "伙伴聊天",
    "团队",
    "工作流",
    "自动任务",
  ]);
});

it("keeps the prior snapshot and page depth until the replacement is ready", async () => {
  const newFirst = deferred();
  const newSecond = deferred();
  fixture.groups.mockImplementation(async (snapshot, cursor, _, groupBy) => {
    if (groupBy === "provider") return { ...page(snapshot, ""), groups: [] };
    if (snapshot === "old")
      return cursor
        ? page("old", "2026-11-02")
        : page("old", "2026-11-01", "old cursor");
    return cursor ? newSecond.promise : newFirst.promise;
  });
  const refresh = openPage();
  const header = within(screen.getByRole("banner"));
  expect(header.getByRole("button", { name: "日期范围" })).toBeInTheDocument();
  expect(header.getByRole("button", { name: "全部伙伴" })).toBeInTheDocument();
  expect(
    header.queryByRole("combobox", { name: "时区" }),
  ).not.toBeInTheDocument();
  expect(await screen.findByText("2026-11-01")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "加载更多" }));
  expect(await screen.findByText("2026-11-02")).toBeInTheDocument();

  await refresh();
  await waitFor(() =>
    expect(fixture.groups).toHaveBeenCalledWith(
      "new",
      "",
      expect.any(AbortSignal),
      "day",
    ),
  );
  expect(screen.getByText("2026-11-02")).toBeInTheDocument();
  expect(screen.getByText("11 次调用")).toBeInTheDocument();
  expect(screen.queryByText("22 次调用")).not.toBeInTheDocument();

  newFirst.resolve(page("new", "2026-11-03", "new cursor"));
  await waitFor(() =>
    expect(fixture.groups).toHaveBeenCalledWith(
      "new",
      "new cursor",
      expect.any(AbortSignal),
      "day",
    ),
  );
  expect(screen.getByText("2026-11-02")).toBeInTheDocument();
  expect(screen.queryByText("2026-11-03")).not.toBeInTheDocument();
  expect(fixture.groups).not.toHaveBeenCalledWith(
    "new",
    "old cursor",
    expect.any(AbortSignal),
    "day",
  );

  newSecond.resolve(page("new", "2026-11-04"));
  expect(await screen.findByText("2026-11-04")).toBeInTheDocument();
  expect(screen.getByText("2026-11-03")).toBeInTheDocument();
  expect(screen.getByText("22 次调用")).toBeInTheDocument();
  expect(screen.queryByText("2026-11-01")).not.toBeInTheDocument();
});

it("retains the published overview and rows when replacement groups fail", async () => {
  fixture.groups.mockImplementation(async (snapshot, _, __, groupBy) => {
    if (groupBy === "provider") return { ...page(snapshot, ""), groups: [] };
    if (snapshot === "old") return page("old", "2026-11-05");
    throw new Error("group unavailable");
  });
  const refresh = openPage();
  expect(await screen.findByText("2026-11-05")).toBeInTheDocument();

  await refresh();
  expect(await screen.findByRole("alert")).toBeInTheDocument();
  expect(screen.getByText("2026-11-05")).toBeInTheDocument();
  expect(screen.getByText("11 次调用")).toBeInTheDocument();
  expect(screen.queryByText("22 次调用")).not.toBeInTheDocument();
});

it("keeps the trend and runtime shares on the same snapshot as model statistics", async () => {
  const newModels = deferred();
  fixture.groups.mockImplementation(async (snapshot, _, __, groupBy) => {
    if (groupBy === "provider")
      return {
        ...page(snapshot, ""),
        groups: [
          { key: "opencode", metrics: { totalWithCacheRead: 0 } },
          {
            key: "codex",
            metrics: { knownTotalWithCacheRead: snapshot === "old" ? 150 : 50 },
          },
          { key: "cursor", metrics: { invocations: 1 } },
        ],
      };
    if (groupBy === "day")
      return {
        ...page(snapshot, "2026-11-01"),
        groups: [
          {
            key: "2026-11-01",
            metrics: { totalWithCacheRead: snapshot === "old" ? 123 : 456 },
          },
        ],
      };
    if (snapshot === "new") return newModels.promise;
    return {
      ...page(snapshot, "dfmodel"),
      groups: [
        { key: "dfmodel", provider: "qoder", metrics: { invocations: 1 } },
      ],
    };
  });
  const refresh = openPage(`${novemberRange}&groupBy=model`);
  expect(
    await screen.findByRole("row", { name: /DeepSeek-Flash qoder/ }),
  ).toBeInTheDocument();
  expect(
    await screen.findByRole("group", { name: "用量趋势" }),
  ).toBeInTheDocument();
  const oldDay = { name: "11月1日 · 123 Token" };
  const newDay = { name: "11月1日 · 456 Token" };
  expect(screen.getByRole("button", oldDay)).toBeInTheDocument();
  expect(await screen.findByText("占已记录 Token 的 75%")).toBeInTheDocument();
  expect(screen.getByText("占已记录 Token 的 0%")).toBeInTheDocument();
  const providerList = screen.getByRole("list", { name: "智能体用量" });
  const [first, second, third] = within(providerList).getAllByRole("listitem");
  expect(first).toHaveTextContent(/Codex.*75%.*150/);
  expect(second).toHaveTextContent(/OpenCode.*0%.*0/);
  expect(third).toHaveTextContent("未知");
  expect(third).not.toHaveTextContent("%");

  await refresh();
  expect(screen.getByRole("button", oldDay)).toBeInTheDocument();
  expect(screen.getByText("占已记录 Token 的 75%")).toBeInTheDocument();
  expect(screen.queryByText("占已记录 Token 的 25%")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", newDay)).not.toBeInTheDocument();

  newModels.resolve(page("new", "new model"));
  expect(await screen.findByText("new model")).toBeInTheDocument();
  expect(await screen.findByRole("button", newDay)).toBeInTheDocument();
  expect(await screen.findByText("占已记录 Token 的 25%")).toBeInTheDocument();
  expect(screen.queryByRole("button", oldDay)).not.toBeInTheDocument();
});
