import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { KnowledgeWikiEditor } from "./knowledge-wiki-editor";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import type { WikiPage } from "./knowledge-wiki-queries";

const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

it("编辑中收到同版内容更新仍提交打开时的读取时间", async () => {
  const query = new QueryClient();
  const commands: KnowledgeWikiCommands = new Map();
  const saved = vi.fn();
  let submitted: Record<string, unknown> | undefined;
  server.use(
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/pages/page",
      async ({ request }) => {
        submitted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ resourceId: "page", state: "saved" });
      },
    ),
  );
  const editor = (page: WikiPage) => (
    <QueryClientProvider client={query}>
      <KnowledgeWikiEditor
        resource={{
          backend: "local",
          connectionId: "local",
          knowledgeBaseId: "kb",
        }}
        kind="edit"
        page={page}
        themes={[]}
        parentId=""
        structureVersion={1}
        commands={commands}
        commandKey="edit/page"
        onClose={vi.fn()}
        onSaved={saved}
      />
    </QueryClientProvider>
  );
  const page = {
    id: "page",
    slug: "concept/page",
    title: "页面",
    content: "原始正文",
    pageType: "concept",
    status: "published",
    version: 2,
    updatedAt: 1000,
  };
  const view = render(editor(page));
  fireEvent.change(screen.getByRole("textbox", { name: "正文" }), {
    target: { value: "用户草稿" },
  });
  view.rerender(
    editor({ ...page, content: "后台补充的正文", updatedAt: 2000 }),
  );
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(submitted).toMatchObject({
    expectedVersion: 2,
    page: { content: "用户草稿", updatedAt: 1000 },
  });
});
