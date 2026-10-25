import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { AppRouteScene } from "@/app/scenes/app-route-scene";

vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
const reads: URL[] = [];
const clients: QueryClient[] = [];
const server = setupServer(
  http.get(
    "http://plugin-skill-page.test/api/v1/plugins/p/skill",
    ({ request }) => {
      reads.push(new URL(request.url));
      return HttpResponse.json({
        skill: { key: "guide", name: "操作指南", description: "整理工作资料" },
        content: "---\nname: guide\n---\n# 使用指南\n\n阅读并整理资料。",
        resources: [{ path: "SKILL.md" }],
        version: { id: "pinned", version: "1.0.0" },
        versions: [{ id: "pinned", version: "1.0.0" }],
      });
    },
  ),
  http.get("http://plugin-skill-page.test/api/v1/plugins/p", () =>
    HttpResponse.json({
      plugin: { id: "p", name: "办公" },
      versions: [{ id: "pinned", package: { manifest: { name: "office" } } }],
    }),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://plugin-skill-page.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  cleanup();
  clients.forEach((item) => item.clear());
  clients.length = 0;
  reads.length = 0;
});
afterAll(() => server.close());

function Scene() {
  const location = useLocation();
  if (
    location.pathname === "/resources" ||
    location.pathname.endsWith("/resources")
  )
    return (
      <div>
        返回结果 {location.pathname}
        {location.search}
      </div>
    );
  return (
    <AppRouteScene
      location={location.pathname + location.search}
      outletContext={{
        chatRightPanel: undefined,
        onTeamThreadDeleted: vi.fn(),
        setChatRightPanel: vi.fn(),
      }}
    />
  );
}

it.each(["", "w1"])(
  "opens the %s plugin skill as a read-only detail page and returns to its skill list",
  async (wakerId) => {
    const base = wakerId ? `/wakers/${wakerId}` : "/resources";
    const back = wakerId
      ? `/wakers/${wakerId}/resources?category=skills`
      : "/resources?category=skills";
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    clients.push(queryClient);
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[
            {
              pathname: `${base}/plugins/p/skills/guide`,
              search: "?version=pinned",
              state: { resourceBack: back },
            },
          ]}
        >
          <Scene />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("heading", { name: "使用指南" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "操作指南" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "元数据" })).toBeVisible();
    expect(await screen.findByText("办公")).toBeVisible();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /编辑|更新|卸载|删除/ }),
    ).not.toBeInTheDocument();
    expect(reads[0]?.searchParams.get("versionId")).toBe("pinned");
    expect(reads[0]?.searchParams.get("key")).toBe("guide");
    expect(reads[0]?.searchParams.get("wakerId")).toBe(wakerId || null);
    fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText(`返回结果 ${back}`)).toBeVisible();
  },
);
