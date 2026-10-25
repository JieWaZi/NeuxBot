import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll, afterEach, expect, it } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { KnowledgeEmbeddingFields } from "./knowledge-embedding-fields";
import {
  emptyEmbeddingDraft,
  type KnowledgeEmbeddingDraft,
} from "./knowledge-embedding-profile";
const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: "http://ally.test" });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
function show(initial = emptyEmbeddingDraft(), libraryId?: string) {
  function Form() {
    const [value, onChange] = useState<KnowledgeEmbeddingDraft>(initial);
    return (
      <KnowledgeEmbeddingFields
        value={value}
        onChange={onChange}
        libraryId={libraryId}
      />
    );
  }
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <Form />
    </QueryClientProvider>,
  );
}
it("新建直接测试独立Embedding草稿，不先保存且不调用Memory", async () => {
  const bodies: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/embedding/test",
      async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ dimensions: 1536 });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  await user.type(
    screen.getByRole("textbox", { name: "服务 URL" }),
    "https://kb.example/v1",
  );
  await user.type(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
    "embed-kb",
  );
  await user.type(screen.getByLabelText("API Key"), "only-kb-key");
  await user.click(screen.getByRole("button", { name: "测试连接" }));
  expect(await screen.findByRole("status")).toHaveTextContent("向量维度：1536");
  expect(bodies).toEqual([
    {
      baseUrl: "https://kb.example/v1",
      model: "embed-kb",
      apiKey: "only-kb-key",
    },
  ]);
});
it("编辑测试空密钥只沿用指定知识库已保存密钥，改字段清除旧测试结果", async () => {
  const bodies: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/embedding/test",
      async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ dimensions: 768 });
      },
    ),
  );
  const user = userEvent.setup();
  show(
    {
      baseUrl: "https://own.example/v1",
      model: "own-model",
      apiKey: "",
      hasApiKey: true,
    },
    "real-library",
  );
  expect(screen.getByLabelText("API Key")).toHaveValue("");
  await user.click(screen.getByRole("button", { name: "测试连接" }));
  await screen.findByText("向量维度：768");
  expect(bodies).toEqual([
    {
      libraryId: "real-library",
      baseUrl: "https://own.example/v1",
      model: "own-model",
    },
  ]);
  await user.type(
    screen.getByRole("textbox", { name: "服务 URL" }),
    "/changed",
  );
  expect(screen.queryByText("向量维度：768")).not.toBeInTheDocument();
});
