import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { KnowledgeWikiReferences } from "./knowledge-wiki-references";
import type { WikiPage } from "./knowledge-wiki-queries";

vi.mock("./knowledge-result-document", () => ({
  KnowledgeResultDocument: ({ sourceId }: { sourceId: string }) => (
    <div role="dialog" aria-label="原件预览">
      {sourceId}
    </div>
  ),
}));
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: "http://ally.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
const resource = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
function show(page: WikiPage) {
  const onLink = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <KnowledgeWikiReferences
        resource={resource}
        page={page}
        pages={[
          { id: "other", slug: "concept/other", title: "引用此页的知识" },
        ]}
        onLink={onLink}
      />
    </QueryClientProvider>,
  );
  return onLink;
}
it("展示权威反向链接并按不可变来源版本读取标题和打开原件", async () => {
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources/version",
      () =>
        HttpResponse.json({
          id: "version",
          sourceId: "logical-source",
          filename: "拨测工具.md",
        }),
    ),
  );
  const onLink = show({
    inLinks: ["concept/other"],
    sourceRefs: ["version", "version"],
  });
  const footer = await screen.findByRole("contentinfo", { name: "页面引用" });
  await userEvent.click(
    within(footer).getByRole("button", { name: "引用此页的知识" }),
  );
  expect(onLink).toHaveBeenCalledWith("concept/other");
  const links = await within(footer).findAllByRole("button", {
    name: "拨测工具.md",
  });
  expect(links).toHaveLength(1);
  await userEvent.click(
    within(footer).getByRole("button", { name: "拨测工具.md" }),
  );
  expect(screen.getByRole("dialog", { name: "原件预览" })).toHaveTextContent(
    "version",
  );
});
it("解析官方远端带标题的来源合同而不把标题作为资源身份", async () => {
  show({ sourceRefs: ["document-id|资料标题.md"] });
  await userEvent.click(screen.getByRole("button", { name: "资料标题.md" }));
  expect(screen.getByRole("dialog", { name: "原件预览" })).toHaveTextContent(
    "document-id",
  );
});
it("来源不可用时仍可进入统一预览查看失败，不隐藏已有引用", async () => {
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources/missing",
      () => new HttpResponse(null, { status: 404 }),
    ),
  );
  show({ sourceRefs: ["missing"] });
  await userEvent.click(screen.getByRole("button", { name: "来源文档 1" }));
  expect(screen.getByRole("dialog", { name: "原件预览" })).toHaveTextContent(
    "missing",
  );
});

it("被链接与来源文档各显示前 30 个，可独立展开和收起更多引用", async () => {
  const onLink = show({
    inLinks: Array.from(
      { length: 31 },
      (_, index) => `concept/page-${index + 1}`,
    ),
    sourceRefs: Array.from(
      { length: 31 },
      (_, index) => `version-${index + 1}|资料-${index + 1}.md`,
    ),
  });
  const incoming = screen.getByRole("region", { name: "被链接" });
  const sources = screen.getByRole("region", { name: "来源文档" });
  expect(
    within(incoming).getByRole("button", { name: "concept/page-30" }),
  ).toBeVisible();
  expect(
    within(incoming).queryByRole("button", { name: "concept/page-31" }),
  ).toBeNull();
  expect(
    within(sources).getByRole("button", { name: "资料-30.md" }),
  ).toBeVisible();
  expect(
    within(sources).queryByRole("button", { name: "资料-31.md" }),
  ).toBeNull();
  await userEvent.click(within(incoming).getByRole("button", { name: "更多" }));
  await userEvent.click(
    within(incoming).getByRole("button", { name: "concept/page-31" }),
  );
  expect(onLink).toHaveBeenCalledWith("concept/page-31");
  expect(
    within(sources).queryByRole("button", { name: "资料-31.md" }),
  ).toBeNull();
  await userEvent.click(within(sources).getByRole("button", { name: "更多" }));
  await userEvent.click(
    within(sources).getByRole("button", { name: "资料-31.md" }),
  );
  expect(screen.getByRole("dialog", { name: "原件预览" })).toHaveTextContent(
    "version-31",
  );
  await userEvent.click(within(incoming).getByRole("button", { name: "收起" }));
  expect(
    within(incoming).queryByRole("button", { name: "concept/page-31" }),
  ).toBeNull();
});
