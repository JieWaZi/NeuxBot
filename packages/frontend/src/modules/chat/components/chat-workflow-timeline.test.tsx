import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { listChatWorkflowRunsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListChatWorkflowRunsResponse } from "@/shared/api/generated/types.gen";
import { WorkbenchProvider, useWorkbench } from "../workbench/workbench-store";
import { useChatWorkflowTimeline } from "./chat-workflow-timeline";

const questionModule = vi.hoisted(() => {
  let release = () => {};
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { ready, release };
});
vi.mock(
  import("@/modules/workflow/components/human-request"),
  async (importOriginal) => {
    await questionModule.ready;
    return importOriginal();
  },
);

function Timeline({
  completed = false,
  replyStatus = completed ? "completed" : undefined,
  durationMs = 10,
}: {
  completed?: boolean;
  replyStatus?: "completed" | "failed";
  durationMs?: number;
}) {
  const { events } = useChatWorkflowTimeline({
    chatId: "chat-1",
    scopeKey: "chat:chat-1",
    active: false,
    messages: [
      { id: "input-1", role: "user", executionId: "execution-1" },
      ...(replyStatus
        ? [
            {
              id: "reply-1",
              role: "assistant",
              executionId: "execution-1",
              status: replyStatus,
              runMetrics: { startedAt: 100, durationMs },
            } as const,
          ]
        : []),
    ],
    onOpenWorkbench: () => undefined,
  });
  return events.map(({ id, content }) => <div key={id}>{content}</div>);
}

it("已结束的伙伴回复保留尚在运行的工作流提示", () => {
  const client = new QueryClient();
  client.setQueryData<ListChatWorkflowRunsResponse>(
    listChatWorkflowRunsQueryKey({
      path: { chatId: "chat-1" },
      query: { executionIds: "execution-1" },
    }),
    {
      resources: [
        {
          id: "run-1",
          definitionId: "review",
          executionId: "execution-1",
          name: "代码评审",
          status: "running",
        },
      ],
    },
  );
  const view = render(
    <QueryClientProvider client={client}>
      <WorkbenchProvider>
        <Timeline completed />
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  try {
    expect(screen.getByText("伙伴回复中断，工作流仍在处理。")).toBeVisible();
  } finally {
    view.unmount();
    client.clear();
  }
});

it.each([
  ["completed", 10, undefined, true],
  ["completed", 30, undefined, false],
  ["failed", 30, undefined, true],
  ["failed", 30, 200, false],
] as const)(
  "原回复状态 %s、耗时 %i、补写时间 %s 决定补写入口",
  (replyStatus, durationMs, summaryFinishedAt, canSupplement) => {
    const client = new QueryClient();
    client.setQueryData(
      listChatWorkflowRunsQueryKey({
        path: { chatId: "chat-1" },
        query: { executionIds: "execution-1" },
      }),
      {
        resources: [
          {
            id: "run-1",
            definitionId: "review",
            executionId: "execution-1",
            name: "代码评审",
            status: "succeeded",
            completedAt: 120,
            summaryFinishedAt,
          },
        ],
      },
    );
    const view = render(
      <QueryClientProvider client={client}>
        <WorkbenchProvider>
          <Timeline replyStatus={replyStatus} durationMs={durationMs} />
        </WorkbenchProvider>
      </QueryClientProvider>,
    );
    try {
      const action = screen.queryByRole("button", { name: "补写总结" });
      if (canSupplement) expect(action).toBeVisible();
      else expect(action).not.toBeInTheDocument();
    } finally {
      view.unmount();
      client.clear();
    }
  },
);

it("用局部骨架占位直至工作流回答卡加载完成", async () => {
  const client = new QueryClient();
  client.setQueryData<ListChatWorkflowRunsResponse>(
    listChatWorkflowRunsQueryKey({
      path: { chatId: "chat-1" },
      query: { executionIds: "execution-1" },
    }),
    {
      resources: [
        {
          id: "run-1",
          definitionId: "review",
          executionId: "execution-1",
          name: "代码评审",
          status: "waiting",
          activeRequests: [
            {
              kind: "human",
              requestId: "question-1",
              definitionId: "review",
              runId: "run-1",
              human: {
                id: "question-1",
                status: "waiting",
                snapshotDigest: "snapshot-1",
                prompt: "确认评审结果",
                options: { kind: "text" },
              },
            },
          ],
        },
      ],
    },
  );
  const view = render(
    <QueryClientProvider client={client}>
      <WorkbenchProvider>
        <Timeline />
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  try {
    const loading = screen.getByRole("status", { name: "正在读取工作流…" });
    expect(loading).toHaveAttribute("aria-busy", "true");
    expect(loading.querySelector(".agent-grid-spinner")).toBeNull();
    expect(loading.querySelector(".animate-pulse")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /代码评审/ })).toBeVisible();
    expect(screen.queryByText("确认评审结果")).not.toBeInTheDocument();

    questionModule.release();
    expect(await screen.findByText("确认评审结果")).toBeVisible();
    expect(loading).not.toBeInTheDocument();
  } finally {
    view.unmount();
    questionModule.release();
    client.clear();
  }
});

it("运行记录移除后保留名称和可见状态", () => {
  const client = new QueryClient();
  client.setQueryData<ListChatWorkflowRunsResponse>(
    listChatWorkflowRunsQueryKey({
      path: { chatId: "chat-1" },
      query: { executionIds: "execution-1" },
    }),
    {
      resources: [
        {
          id: "run-1",
          definitionId: "review",
          executionId: "execution-1",
          name: "代码评审",
          status: "succeeded",
          removed: true,
        },
      ],
    },
  );
  const view = render(
    <QueryClientProvider client={client}>
      <WorkbenchProvider>
        <Timeline />
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  try {
    expect(screen.getByText("代码评审")).toBeVisible();
    expect(screen.getByText("执行记录已移除")).toBeVisible();
    expect(screen.queryByRole("button", { name: /代码评审/ })).toBeNull();
  } finally {
    view.unmount();
    client.clear();
  }
});

it("父运行标记和子流程问题分别打开真实运行与节点", async () => {
  const user = userEvent.setup();
  const client = new QueryClient();
  function WorkbenchTargets() {
    const { snapshot } = useWorkbench("chat:chat-1");
    return snapshot.tabs.map((tab) => (
      <output key={tab.id} data-testid={tab.contentId}>
        {JSON.stringify(tab.contentReference?.ids)}
      </output>
    ));
  }
  const queryKey = listChatWorkflowRunsQueryKey({
    path: { chatId: "chat-1" },
    query: { executionIds: "execution-1" },
  });
  client.setQueryData<ListChatWorkflowRunsResponse>(queryKey, {
    resources: [
      {
        id: "run-parent",
        definitionId: "flow-parent",
        executionId: "execution-1",
        name: "代码评审",
        status: "waiting",
        activeRequests: [
          {
            kind: "human",
            requestId: "question-child",
            definitionId: "flow-child",
            runId: "run-child",
            instanceId: "instance-child",
            human: {
              id: "question-child",
              status: "waiting",
              prompt: "确认子流程结果",
              snapshotDigest: "child-snapshot",
              options: { kind: "confirm" },
            },
          },
        ],
      },
    ],
  });
  localStorage.removeItem("bot.right-workbench.chat:chat-1");
  const { unmount } = render(
    <QueryClientProvider client={client}>
      <WorkbenchProvider>
        <Timeline />
        <WorkbenchTargets />
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  try {
    await user.click(screen.getByRole("button", { name: /代码评审/ }));
    expect(
      JSON.parse(
        screen.getByTestId("workflow-run:run-parent").textContent ?? "",
      ),
    ).toEqual({
      definitionId: "flow-parent",
      runId: "run-parent",
      chatId: "chat-1",
    });

    questionModule.release();
    await user.click(await screen.findByRole("button", { name: "查看子运行" }));
    expect(
      JSON.parse(
        screen.getByTestId("workflow-run:run-child").textContent ?? "",
      ),
    ).toEqual({
      definitionId: "flow-child",
      runId: "run-child",
      instanceId: "instance-child",
      chatId: "chat-1",
    });
  } finally {
    unmount();
    client.clear();
    localStorage.removeItem("bot.right-workbench.chat:chat-1");
  }
});

it.each([false, true])(
  "快照不可用时仅在另有交付错误时提供补交 (复制失败 %s)",
  (copyFailed) => {
    const client = new QueryClient();
    client.setQueryData(
      listChatWorkflowRunsQueryKey({
        path: { chatId: "chat-1" },
        query: { executionIds: "execution-1" },
      }),
      {
        resources: [
          {
            id: "run-1",
            definitionId: "review",
            executionId: "execution-1",
            name: "代码评审",
            status: "succeeded",
            snapshotError: "UNKNOWN_SNAPSHOT_ERROR",
            deliveryError: copyFailed ? "UNKNOWN_COPY_ERROR" : undefined,
          },
        ],
      },
    );
    const { unmount } = render(
      <QueryClientProvider client={client}>
        <WorkbenchProvider>
          <Timeline />
        </WorkbenchProvider>
      </QueryClientProvider>,
    );
    try {
      expect(screen.getByText("部分工作流原始文件不可用。")).toBeVisible();
      const retry = screen.queryByRole("button", { name: "补交附件" });
      if (copyFailed) expect(retry).toBeVisible();
      else expect(retry).not.toBeInTheDocument();
    } finally {
      unmount();
      client.clear();
    }
  },
);
