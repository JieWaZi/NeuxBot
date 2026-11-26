import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";

import { TooltipProvider } from "@/shared/ui/tooltip";
import { client } from "@/shared/api/generated/client.gen";
import { WakerMemoryPage } from "./waker-memory-page";
import "@/shared/i18n";

vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: React.ReactNode }) => children,
}));

const base = "http://127.0.0.1:8080";
const path = `${base}/api/v1/memory/wakers/waker-1/memories`;
const readProjects = vi.fn();
const server = setupServer(
  http.get(`${base}/api/v1/wakers/:wakerId/projects`, () => {
    readProjects();
    return HttpResponse.json({ resources: [], totalSize: 0 });
  }),
);
beforeAll(() => {
  client.setConfig({ baseUrl: base });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  readProjects.mockClear();
});
afterAll(() => server.close());

function renderPage(search = "") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[`/wakers/waker-1/memory${search}`]}>
          <Routes>
            <Route
              path="/wakers/:wakerId/memory"
              element={<WakerMemoryPage />}
            />
          </Routes>
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return userEvent.setup();
}

function group(name: string) {
  return within(screen.getByRole("region", { name }));
}

it("filters both memory groups and paginates each independently", async () => {
  const reads: Array<{ core: string | null; page: string | null }> = [];
  server.use(
    http.get(path, ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (
        params.get("query") !== "偏好" ||
        !params.has("updatedAtStart") ||
        !params.has("updatedAtEnd")
      ) {
        return new HttpResponse(null, { status: 400 });
      }
      reads.push({ core: params.get("core"), page: params.get("pageNum") });
      const core = params.get("core") === "true";
      return HttpResponse.json({
        pageNum: 1,
        pageSize: Number(params.get("pageSize")),
        resources: [
          {
            id: core ? "core" : "regular",
            content: core ? "核心偏好" : "普通偏好",
            core,
          },
        ],
        totalSize: 40,
      });
    }),
  );

  const user = renderPage(
    "?search=%E5%81%8F%E5%A5%BD&from=2026-11-01&to=2026-11-20",
  );
  const core = await screen.findByRole("region", { name: "核心记忆" });
  const regular = screen.getByRole("region", { name: "普通记忆" });
  expect(await within(core).findByText("核心偏好")).toBeVisible();
  expect(await within(regular).findByText("普通偏好")).toBeVisible();
  expect(
    core.compareDocumentPosition(regular) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  await user.click(within(regular).getByRole("button", { name: "下一页" }));
  await waitFor(() =>
    expect(reads).toContainEqual({ core: "false", page: "2" }),
  );
  expect(reads).toContainEqual({ core: "true", page: "1" });
  expect(reads).not.toContainEqual({ core: "true", page: "2" });
});

it("returns to the last available page when the final item moves to the core group", async () => {
  let moved = false;
  server.use(
    http.get(path, ({ request }) => {
      const params = new URL(request.url).searchParams;
      const core = params.get("core") === "true";
      const page = Number(params.get("pageNum"));
      return HttpResponse.json({
        pageNum: page,
        pageSize: 20,
        resources: core
          ? moved
            ? [{ id: "last", content: "末页记忆", core: true }]
            : []
          : page === 1
            ? [{ id: "first", content: "第一页记忆", core: false }]
            : moved
              ? []
              : [{ id: "last", content: "末页记忆", core: false }],
        totalSize: core ? (moved ? 1 : 0) : moved ? 20 : 21,
      });
    }),
    http.put(`${path}/last/core`, () => {
      moved = true;
      return HttpResponse.json({ id: "last", content: "末页记忆", core: true });
    }),
  );
  const user = renderPage("?regularPage=2");
  await user.click(
    await group("普通记忆").findByRole("button", { name: "设为核心记忆" }),
  );
  expect(await group("普通记忆").findByText("第一页记忆")).toBeVisible();
});

it("creates directly for the current partner and offers settings for that partner", async () => {
  let created: unknown;
  server.use(
    http.get(path, () => HttpResponse.json({ resources: [], totalSize: 0 })),
    http.post(path, async ({ request }) => {
      created = await request.json();
      return HttpResponse.json({ id: "memory-1" });
    }),
  );
  const user = renderPage();
  await user.click(screen.getByRole("button", { name: "新建记忆" }));
  const dialog = screen.getByRole("dialog", { name: "新建记忆" });
  await user.type(within(dialog).getByLabelText("记忆内容"), "使用中文");
  await user.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(dialog).not.toBeInTheDocument());
  expect(created).toEqual({ content: "使用中文" });
  expect(readProjects).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "设置" })).toHaveAttribute(
    "href",
    "/settings/agents?detail=partner-review&tab=memory&wakerId=waker-1",
  );
});

it("only shows the core group while it contains a memory and restores move focus", async () => {
  let core = false;
  server.use(
    http.get(path, ({ request }) => {
      const visible =
        (new URL(request.url).searchParams.get("core") === "true") === core;
      return HttpResponse.json({
        resources: visible
          ? [{ id: "memory-1", content: "记忆内容", core }]
          : [],
        totalSize: visible ? 1 : 0,
      });
    }),
    http.put(`${path}/memory-1/core`, async ({ request }) => {
      const body = await request.json();
      if (
        !body ||
        typeof body !== "object" ||
        !("core" in body) ||
        typeof body.core !== "boolean"
      ) {
        return new HttpResponse(null, { status: 400 });
      }
      core = body.core;
      return HttpResponse.json({ id: "memory-1", content: "记忆内容", core });
    }),
  );

  const user = renderPage();
  const action = await group("普通记忆").findByRole("button", {
    name: "设为核心记忆",
  });
  expect(screen.queryByRole("region", { name: "核心记忆" })).toBeNull();
  action.focus();
  await user.keyboard("{Enter}");
  const coreGroup = await screen.findByRole("region", { name: "核心记忆" });
  const destination = await within(coreGroup).findByRole("button", {
    name: "设为普通记忆",
  });
  await waitFor(() => expect(destination).toHaveFocus());
  expect(screen.getAllByText("记忆内容")).toHaveLength(1);
  expect(group("普通记忆").queryByText("记忆内容")).not.toBeInTheDocument();

  await user.keyboard("{Enter}");
  const restored = await group("普通记忆").findByRole("button", {
    name: "设为核心记忆",
  });
  await waitFor(() => expect(restored).toHaveFocus());
  expect(screen.queryByRole("region", { name: "核心记忆" })).toBeNull();
});

it("keeps the source row when the core budget rejects the change", async () => {
  server.use(
    http.get(path, ({ request }) => {
      const regular = new URL(request.url).searchParams.get("core") === "false";
      return HttpResponse.json({
        resources: regular
          ? [{ id: "memory-1", content: "记忆内容", core: false }]
          : [],
        totalSize: regular ? 1 : 0,
      });
    }),
    http.put(
      `${path}/memory-1/core`,
      () => new HttpResponse(null, { status: 400 }),
    ),
  );
  const user = renderPage();
  await user.click(
    await group("普通记忆").findByRole("button", { name: "设为核心记忆" }),
  );
  expect(await group("普通记忆").findByRole("alert")).toBeVisible();
  expect(group("普通记忆").getByText("记忆内容")).toBeVisible();
  expect(screen.queryByRole("region", { name: "核心记忆" })).toBeNull();
});
