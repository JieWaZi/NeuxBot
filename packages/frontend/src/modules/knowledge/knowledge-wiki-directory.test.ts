import { QueryClient } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { wikiPageOptions, wikiPagesOptions } from "./knowledge-wiki-queries";

const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://directory.test" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
const targets = [
  { backend: "local", connectionId: "local", knowledgeBaseId: "kb" },
  { backend: "custom", connectionId: "remote", knowledgeBaseId: "kb" },
  { backend: "integration", connectionId: "remote", knowledgeBaseId: "kb" },
  { backend: "weknora", connectionId: "remote", knowledgeBaseId: "kb" },
] as const;
function root(ref: (typeof targets)[number]) {
  return ref.backend === "local"
    ? "http://directory.test/api/v1/knowledge/libraries/kb"
    : "http://directory.test/api/v1/knowledge/connections/remote/libraries/kb";
}
it.each(targets)("$backend 目录只读轻量接口，详情独立请求", async (ref) => {
  let directoryReads = 0;
  let detailReads = 0;
  server.use(
    http.get(`${root(ref)}/page-directory`, ({ request }) => {
      directoryReads++;
      expect(new URL(request.url).searchParams.get("limit")).toBe("1000");
      return HttpResponse.json({
        items: Array.from({ length: 500 }, (_, index) => ({
          id: `p-${index}`,
          slug: `concept/${index}`,
          title: `页面 ${index}`,
          pageType: "concept",
          version: 1,
        })),
        total: 500,
      });
    }),
    http.get(`${root(ref)}/pages/p-7`, () => {
      detailReads++;
      return HttpResponse.json({ id: "p-7", content: "点击后的正文" });
    }),
  );
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  try {
    const rows = await query.fetchQuery(wikiPagesOptions(ref));
    expect(rows).toHaveLength(500);
    expect(directoryReads).toBe(1);
    expect(detailReads).toBe(0);
    const page = await query.fetchQuery(
      wikiPageOptions(ref, "p-7", "concept/7"),
    );
    expect(page.content).toBe("点击后的正文");
    expect(detailReads).toBe(1);
  } finally {
    query.clear();
  }
});
it.each(["complete", "empty", "duplicate"])(
  "目录跨批次完整性：%s",
  async (mode) => {
    const ref = targets[0];
    const offsets: number[] = [];
    server.use(
      http.get(`${root(ref)}/page-directory`, ({ request }) => {
        const offset = Number(new URL(request.url).searchParams.get("offset"));
        offsets.push(offset);
        const count = offset === 0 ? 1000 : mode === "empty" ? 0 : 1;
        return HttpResponse.json({
          items: Array.from({ length: count }, (_, i) => ({
            id: `p-${mode === "duplicate" && offset ? 0 : offset + i}`,
          })),
          total: 1001,
        });
      }),
    );
    const query = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    try {
      const result = query.fetchQuery(wikiPagesOptions(ref));
      if (mode === "complete") expect(await result).toHaveLength(1001);
      else await expect(result).rejects.toThrow();
      expect(offsets).toEqual([0, 1000]);
    } finally {
      query.clear();
    }
  },
);
