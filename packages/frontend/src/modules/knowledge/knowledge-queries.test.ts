import { QueryClient } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { organizableSourcesOptions } from "./knowledge-queries";

const requests: Array<{ offset: number; limit: number }> = [];
const server = setupServer(
  http.get(
    "http://selection.test/api/v1/knowledge/libraries/library/sources",
    ({ request }) => {
      const url = new URL(request.url);
      const offset = Number(url.searchParams.get("offset"));
      const limit = Number(url.searchParams.get("limit"));
      requests.push({ offset, limit });
      return HttpResponse.json({
        total: 123,
        items: Array.from(
          { length: Math.max(0, Math.min(limit, 123 - offset)) },
          (_, index) => ({
            id: `version-${offset + index}`,
            sourceId: `source-${offset + index}`,
            filename: "资料.md",
            state: "pending",
          }),
        ),
      });
    },
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://selection.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

it("完整选择使用批量来源页，仍包含首屏之外的全部可整理来源", async () => {
  const queries = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  try {
    const sources = await queries.fetchQuery(
      organizableSourcesOptions({
        backend: "local",
        connectionId: "local",
        knowledgeBaseId: "library",
      }),
    );
    expect(sources).toHaveLength(123);
    expect(sources.at(-1)?.id).toBe("version-122");
    expect(requests).toEqual([
      { offset: 0, limit: 100 },
      { offset: 100, limit: 100 },
    ]);
  } finally {
    queries.clear();
  }
});
