import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { useState } from "react";
import { Blob as NodeBlob, File as NodeFile } from "node:buffer";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { SchemaForm, useSchemaFormValidity } from "./schema-form";

const origin = "http://workflow-file.test";
const server = setupServer();
beforeAll(async () => {
  // 原生 fetch 编码 multipart 时需要具备流和 arrayBuffer 的真实文件对象。
  const nativeForm = await new Response("--test--\r\n", {
    headers: { "Content-Type": "multipart/form-data; boundary=test" },
  }).formData();
  vi.stubGlobal("FormData", nativeForm.constructor);
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("File", NodeFile);
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

function FileForm() {
  const [value, setValue] = useState<unknown>({});
  const validity = useSchemaFormValidity();
  return (
    <>
      <SchemaForm
        compact
        fileWorkflowId="flow"
        schema={{
          type: "object",
          required: ["document"],
          properties: {
            document: { type: "string", format: "file", title: "输入文件" },
          },
        }}
        value={value}
        onChange={setValue}
        onValidityChange={validity.onValidityChange}
      />
      <button disabled={!validity.valid}>运行</button>
      <output data-testid="args">{JSON.stringify(value)}</output>
    </>
  );
}

it("上传实际文件字节后提交引用，上传期间阻止运行", async () => {
  const user = userEvent.setup();
  let complete: (() => void) | undefined;
  const pending = new Promise<void>((resolve) => {
    complete = resolve;
  });
  let uploadedName = "";
  let uploadedText = "";
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/files`, async ({ request }) => {
      const data = await request.formData();
      const file = data.get("file");
      if (file !== null && typeof file !== "string") {
        uploadedName = file.name;
        uploadedText = await file.text();
      }
      await pending;
      return HttpResponse.json({
        reference: "ally-file:123/document.csv",
        name: "document.csv",
        size: 12,
      });
    }),
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <FileForm />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("button", { name: "运行" })).toBeDisabled();
  await user.upload(
    screen.getByLabelText(/输入文件/),
    new File(["a,b\n1,2"], "document.csv", { type: "text/csv" }),
  );
  expect(screen.getByRole("button", { name: "运行" })).toBeDisabled();
  await waitFor(() => expect(uploadedText).toBe("a,b\n1,2"));
  expect(uploadedName).toBe("document.csv");
  complete?.();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "运行" })).toBeEnabled(),
  );
  expect(screen.getByTestId("args")).toHaveTextContent(
    '"document":"ally-file:123/document.csv"',
  );
});

it("上传失败保留表单错误并不允许无文件运行", async () => {
  const user = userEvent.setup();
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/files`, () =>
      HttpResponse.json({ message: "storage failed" }, { status: 500 }),
    ),
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <FileForm />
    </QueryClientProvider>,
  );
  await user.upload(
    screen.getByLabelText(/输入文件/),
    new File(["content"], "document.txt"),
  );
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "运行" })).toBeDisabled();
  expect(screen.getByTestId("args")).toHaveTextContent("{}");
});
