import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor, cleanup } from "@testing-library/react";
import type { ReactNode } from "react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { useInstalledPluginReferences } from "./use-installed-plugin-references";

vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: () => () => {},
}));
const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://references.test" });
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
});
afterAll(() => server.close());

it("重启后尚未取得授权状态也显示已启用安装的固定版本引用", async () => {
  let reads = 0,
    authorizationReads = 0,
    managementReads = 0;
  server.use(
    http.get(
      "http://references.test/api/v1/wakers/w1/plugin-references",
      () => {
        reads++;
        return HttpResponse.json([
          {
            id: "installed",
            name: "办公",
            description: "",
            version: "1.0",
            versionId: "v1",
          },
        ]);
      },
    ),
    http.get("http://references.test/api/v1/wakers/w1/plugins", () => {
      managementReads++;
      return new HttpResponse(null, { status: 503 });
    }),
    http.get(
      "http://references.test/api/v1/plugins/installed/authorization",
      () => {
        authorizationReads++;
        return new HttpResponse(null, { status: 503 });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = renderHook(() => useInstalledPluginReferences("w1"), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  await waitFor(() =>
    expect(view.result.current.plugins).toEqual([
      {
        id: "installed",
        name: "办公",
        description: "",
        version: "1.0",
        versionId: "v1",
      },
    ]),
  );
  expect(reads).toBe(1);
  expect(authorizationReads).toBe(0);
  expect(managementReads).toBe(0);
  view.unmount();
  queryClient.clear();
});
