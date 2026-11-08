import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { ResourceCategoryField } from "./resource-category-field";

const endpoint = "http://127.0.0.1:8080/api/v1/resource-categories";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function mount(type: "plugin" | "skill" | "connector") {
  const change = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ResourceCategoryField
        type={type}
        value={["已有"]}
        onValueChange={change}
      />
    </QueryClientProvider>,
  );
  return change;
}

it.each(["plugin", "skill", "connector"] as const)(
  "reads %s options through the generated client",
  async (type) => {
    const user = userEvent.setup();
    server.use(
      http.get(endpoint, ({ request }) => {
        expect(new URL(request.url).searchParams.get("type")).toBe(type);
        return HttpResponse.json([
          { id: "category", type, name: `${type}分类` },
        ]);
      }),
    );
    const change = mount(type);
    await user.click(screen.getByRole("combobox", { name: "分类" }));
    await user.click(
      await screen.findByRole("option", { name: `${type}分类` }),
    );
    expect(change).toHaveBeenLastCalledWith(["已有", `${type}分类`]);
  },
);

it("retains tags while an option request fails and recovers after retry", async () => {
  const user = userEvent.setup();
  server.use(http.get(endpoint, () => new HttpResponse(null, { status: 500 })));
  mount("skill");
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("button", { name: "移除分类“已有”" })).toBeVisible();
  server.use(
    http.get(endpoint, () => HttpResponse.json([{ name: "恢复分类" }])),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await user.click(screen.getByRole("combobox", { name: "分类" }));
  expect(await screen.findByRole("option", { name: "恢复分类" })).toBeVisible();
});
