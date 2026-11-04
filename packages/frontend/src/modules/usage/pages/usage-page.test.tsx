import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";

import { UsagePage } from "./usage-page";

const fixture = vi.hoisted(() => ({
  snapshot: "old",
  groups:
    vi.fn<
      (
        snapshot: string,
        cursor: string,
        signal: AbortSignal,
      ) => Promise<unknown>
    >(),
}));

vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: () => {} }));
vi.mock("../api/usage-queries", () => ({
  usageSummaryOptions: () => ({
    queryKey: ["usage-summary"],
    queryFn: async () => ({
      snapshot: fixture.snapshot,
      metrics: { invocations: fixture.snapshot === "old" ? 11 : 22 },
    }),
  }),
  usageGroupsOptions: (_filters: unknown, snapshot: string) => ({
    queryKey: ["usage-groups", snapshot],
    queryFn: ({
      signal,
      pageParam,
    }: {
      signal: AbortSignal;
      pageParam: string;
    }) => fixture.groups(snapshot, pageParam, signal),
  }),
}));

const page = (snapshot: string, key: string, nextCursor?: string) => ({
  snapshot,
  metrics: { invocations: snapshot === "old" ? 11 : 22 },
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

function openPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  });
  const page = () => (
    <QueryClientProvider client={client}>
      <UsagePage
        search={new URLSearchParams("timezone=UTC")}
        onSearchChange={() => {}}
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
  fixture.groups.mockReset();
});

it("keeps the prior snapshot and page depth until the replacement is ready", async () => {
  const newFirst = deferred();
  const newSecond = deferred();
  fixture.groups.mockImplementation(async (snapshot, cursor) => {
    if (snapshot === "old")
      return cursor
        ? page("old", "old second")
        : page("old", "old first", "old cursor");
    return cursor ? newSecond.promise : newFirst.promise;
  });
  const refresh = openPage();
  expect(await screen.findByText("old first")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "加载更多" }));
  expect(await screen.findByText("old second")).toBeInTheDocument();

  await refresh();
  await waitFor(() =>
    expect(fixture.groups).toHaveBeenCalledWith(
      "new",
      "",
      expect.any(AbortSignal),
    ),
  );
  expect(screen.getByText("old second")).toBeInTheDocument();
  expect(screen.getByText("11")).toBeInTheDocument();
  expect(screen.queryByText("22")).not.toBeInTheDocument();

  newFirst.resolve(page("new", "new first", "new cursor"));
  await waitFor(() =>
    expect(fixture.groups).toHaveBeenCalledWith(
      "new",
      "new cursor",
      expect.any(AbortSignal),
    ),
  );
  expect(screen.getByText("old second")).toBeInTheDocument();
  expect(screen.queryByText("new first")).not.toBeInTheDocument();
  expect(fixture.groups).not.toHaveBeenCalledWith(
    "new",
    "old cursor",
    expect.any(AbortSignal),
  );

  newSecond.resolve(page("new", "new second"));
  expect(await screen.findByText("new second")).toBeInTheDocument();
  expect(screen.getByText("new first")).toBeInTheDocument();
  expect(screen.getByText("22")).toBeInTheDocument();
  expect(screen.queryByText("old first")).not.toBeInTheDocument();
});

it("retains the published overview and rows when replacement groups fail", async () => {
  fixture.groups.mockImplementation(async (snapshot) => {
    if (snapshot === "old") return page("old", "retained row");
    throw new Error("group unavailable");
  });
  const refresh = openPage();
  expect(await screen.findByText("retained row")).toBeInTheDocument();

  await refresh();
  expect(await screen.findByRole("alert")).toBeInTheDocument();
  expect(screen.getByText("retained row")).toBeInTheDocument();
  expect(screen.getByText("11")).toBeInTheDocument();
  expect(screen.queryByText("22")).not.toBeInTheDocument();
});
