import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll, afterEach, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { emptyLibrary } from "./knowledge-model";
import {
  KnowledgeOrganizeDialog,
  createKnowledgeOrganizeIntent,
} from "./knowledge-organize-dialog";
import { useKnowledgeWrites } from "./knowledge-writes";
const server = setupServer();
const ref = {
  backend: "custom",
  connectionId: "remote",
  knowledgeBaseId: "kb",
};
const root =
  "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
it("统一远端整理由服务器解析配置，不要求客户端 profile", async () => {
  const user = userEvent.setup();
  const accepted = vi.fn();
  const bodies: unknown[] = [];
  server.use(
    http.get(`${root}/capabilities`, () =>
      HttpResponse.json({
        uploadMode: "separate",
        connectionRevision: 2,
        operations: { rebuild: true },
        details: {
          rebuild: {
            supported: true,
            permission: "allowed",
            condition: "ready",
          },
        },
        profiles: [],
      }),
    ),
    http.post(`${root}/organize`, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json({ jobId: "job-a", state: "accepted" });
    }),
  );
  const resource = { ...ref, backend: "integration" };
  function Subject() {
    const operation = useKnowledgeWrites(resource).organize;
    return (
      <KnowledgeOrganizeDialog
        library={{
          ...emptyLibrary("kb", "远端库", "general"),
          ref: resource,
          storage: "remote",
          revision: 2,
          structureVersion: 3,
          sources: [
            {
              id: "source-a",
              sourceId: "source",
              name: "原件.md",
              state: "processed",
              diagnostic: "",
              size: "",
              date: "",
            },
          ],
        }}
        batch={["source-a"]}
        rebuild
        operation={operation}
        onAccepted={accepted}
        onClose={() => {}}
      />
    );
  }
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TooltipProvider>
        <Subject />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "开始整理" })).toBeEnabled(),
  );
  expect(
    screen.queryByRole("combobox", { name: "服务器执行配置" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledOnce());
  expect(bodies[0]).not.toHaveProperty("profileId");
  expect(bodies[0]).toMatchObject({
    sourceVersionIds: ["source-a"],
    expectedVersion: 2,
    structureVersion: 3,
  });
});
it("远端重建使用服务端配置、冻结来源和用户要求，不读取本机Runtime配置", async () => {
  const user = userEvent.setup();
  const accepted = vi.fn();
  const bodies: unknown[] = [];
  server.use(
    http.get(`${root}/capabilities`, () =>
      HttpResponse.json({
        operations: { organize: true, rebuild: true },
        profiles: [{ id: "server-profile", name: "远端执行配置" }],
        details: {
          organize: {
            supported: true,
            permission: "allowed",
            condition: "ready",
          },
          rebuild: {
            supported: true,
            permission: "allowed",
            condition: "ready",
          },
        },
      }),
    ),
    http.post(`${root}/organize`, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json({
        requestId: "req",
        resourceId: "job",
        jobId: "job",
        state: "accepted",
      });
    }),
  );
  const library = {
    ...emptyLibrary("kb", "远端库", "general"),
    ref,
    storage: "remote" as const,
    revision: 4,
    structureVersion: 8,
    sources: [
      {
        id: "source-version",
        sourceId: "source",
        name: "原件.md",
        state: "processed" as const,
        diagnostic: "",
        size: "1KB",
        date: "",
      },
    ],
  };
  const intent = createKnowledgeOrganizeIntent();
  function Subject() {
    const operation = useKnowledgeWrites(ref).organize;
    return (
      <KnowledgeOrganizeDialog
        library={library}
        batch={["source-version"]}
        operation={operation}
        rebuild
        intent={intent}
        onAccepted={accepted}
        onClose={vi.fn()}
      />
    );
  }
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TooltipProvider>
        <Subject />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  expect(
    await screen.findByRole(
      "combobox",
      { name: "服务器执行配置" },
      { timeout: 5000 },
    ),
  ).toBeVisible();
  await user.type(
    screen.getByRole("textbox", { name: "整理提取要求" }),
    "保留术语原文",
  );
  await user.click(screen.getByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(accepted).toHaveBeenCalled());
  expect(bodies).toHaveLength(1);
  expect(bodies[0]).toMatchObject({
    profileId: "server-profile",
    sourceVersionIds: ["source-version"],
    rebuild: true,
    expectedVersion: 4,
    structureVersion: 8,
    instruction: "保留术语原文",
  });
  expect(bodies[0]).not.toHaveProperty("runtimeId");
});

it("未知接纳关闭重开后即使能力读取失败仍可核对原冻结命令", async () => {
  const user = userEvent.setup();
  const accepted = vi.fn();
  const bodies: Record<string, unknown>[] = [];
  let unavailable = false;
  server.use(
    http.get(`${root}/capabilities`, () =>
      unavailable
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({
            operations: { rebuild: true },
            profiles: [{ id: "original-profile", name: "原执行配置" }],
            details: {
              rebuild: {
                supported: true,
                permission: "allowed",
                condition: "ready",
              },
            },
          }),
    ),
    http.get(`${root}/operations/:requestId`, ({ params }) =>
      HttpResponse.json({
        requestId: params.requestId,
        operation: "job.accept",
        ref,
      }),
    ),
    http.post(`${root}/organize`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      bodies.push(body);
      if (bodies.length === 1) return new HttpResponse(null, { status: 503 });
      return HttpResponse.json({
        requestId: body.requestId,
        resourceId: "job",
        jobId: "job",
        state: "accepted",
      });
    }),
  );
  const library = {
    ...emptyLibrary("kb", "远端库", "general"),
    ref,
    storage: "remote" as const,
    revision: 4,
    structureVersion: 8,
  };
  const intent = createKnowledgeOrganizeIntent();
  function Subject({ open }: { open: boolean }) {
    const operation = useKnowledgeWrites(ref).organize;
    return open ? (
      <KnowledgeOrganizeDialog
        library={library}
        batch={["original-source"]}
        operation={operation}
        rebuild
        intent={intent}
        onAccepted={accepted}
        onClose={vi.fn()}
      />
    ) : null;
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (open: boolean) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Subject open={open} />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const rendered = render(view(true));
  await vi.dynamicImportSettled();
  const start = await screen.findByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() => expect(start).toBeEnabled());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(bodies).toHaveLength(1);
  unavailable = true;
  rendered.rerender(view(false));
  rendered.rerender(view(true));
  await waitFor(() =>
    expect(
      queryClient.getQueryState([
        "knowledge",
        "custom",
        "remote",
        "kb",
        "capabilities",
      ])?.status,
    ).toBe("error"),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "开始整理" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledOnce());
  expect(bodies).toHaveLength(2);
  expect(bodies[1]).toEqual(bodies[0]);
});

it("远端接纳中关闭重开不会提前确认成功或重复发送", async () => {
  const user = userEvent.setup();
  const accepted = vi.fn();
  let release: (() => void) | undefined;
  let posts = 0;
  server.use(
    http.get(`${root}/capabilities`, () =>
      HttpResponse.json({
        operations: { rebuild: true },
        profiles: [{ id: "profile", name: "执行配置" }],
        details: {
          rebuild: {
            supported: true,
            permission: "allowed",
            condition: "ready",
          },
        },
      }),
    ),
    http.post(`${root}/organize`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      posts++;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return HttpResponse.json({
        requestId: body.requestId,
        jobId: "job",
        state: "accepted",
      });
    }),
  );
  const library = {
    ...emptyLibrary("kb", "远端库", "general"),
    ref,
    storage: "remote" as const,
    revision: 4,
    structureVersion: 8,
  };
  const intent = createKnowledgeOrganizeIntent();
  function Subject({ open }: { open: boolean }) {
    const operation = useKnowledgeWrites(ref).organize;
    return open ? (
      <KnowledgeOrganizeDialog
        library={library}
        batch={["original-source"]}
        operation={operation}
        rebuild
        intent={intent}
        onAccepted={accepted}
        onClose={vi.fn()}
      />
    ) : null;
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (open: boolean) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Subject open={open} />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const rendered = render(view(true));
  await vi.dynamicImportSettled();
  try {
    const start = await screen.findByRole("button", { name: "开始整理" });
    await waitFor(() => expect(start).toBeEnabled());
    await user.click(start);
    await waitFor(() => expect(posts).toBe(1));
    rendered.rerender(view(false));
    rendered.rerender(view(true));
    await user.click(await screen.findByRole("button", { name: "开始整理" }));
    expect(accepted).not.toHaveBeenCalled();
    expect(posts).toBe(1);
    release?.();
    await waitFor(() => expect(accepted).toHaveBeenCalled());
  } finally {
    release?.();
  }
});
