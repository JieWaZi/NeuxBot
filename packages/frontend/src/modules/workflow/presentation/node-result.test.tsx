import type { GlobalEventObserver } from "@/shared/api/global-events";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import {
  ChatContributionsProvider,
  ChatContributionScope,
  ChatMessageContribution,
} from "@/modules/chat/components/chat-contributions";
import { WorkbenchProvider } from "@/modules/chat";
import {
  NodeOutputCard,
  NodeResultPage,
  ResultOpenScope,
  WorkflowResultMessageScope,
} from "./node-result";
import { useWorkbench } from "@/modules/chat/workbench/workbench-store";
import { catalogDigests } from "@/shared/presentation/catalog-digests.generated";
import {
  getWorkflowChatResults,
  getWorkflowNodeResult,
  listWorkflowNodeResults,
} from "@/shared/api/generated/sdk.gen";
import type * as WorkflowSDK from "@/shared/api/generated/sdk.gen";

vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof WorkflowSDK>()),
  getWorkflowChatResults: vi.fn(),
  listWorkflowNodeResults: vi.fn(),
  getWorkflowNodeResult: vi.fn(async () => ({
    data: {
      present: true,
      reference: ref,
      page: {
        formatVersion: 1,
        catalogVersion: "workflow-result-v1",
        catalogDigest: catalogDigests["workflow-result-v1"],
        title: "处理结果",
        spec: {
          root: "text",
          elements: {
            text: { type: "Text", props: { value: { $state: "/result" } } },
          },
        },
      },
      result: "冻结产出",
      files: [],
    },
  })),
}));
const ref = {
  workflowId: "a".repeat(32),
  runId: "b".repeat(32),
  instanceId: "c".repeat(32),
  attemptId: "d".repeat(32),
  messageId: "final-message",
  title: "处理结果",
};
afterEach(() => vi.clearAllMocks());
function MessageWindow() {
  const [draft, setDraft] = useState("");
  return (
    <>
      <input
        aria-label="草稿"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div data-testid="original">
        <ChatMessageContribution messageId="final-message" />
      </div>
      <div data-testid="continued">
        <ChatMessageContribution messageId="continued-message" />
      </div>
    </>
  );
}

function OutputTabs({ scopeKey }: { scopeKey: string }) {
  const { snapshot } = useWorkbench(scopeKey);
  return (
    <output data-testid="output-tabs">
      {JSON.stringify(
        snapshot.tabs.map((tab) => ({
          reference: tab.contentReference,
          closeable: tab.closeable,
        })),
      )}
    </output>
  );
}

describe("workflow final-message contribution", () => {
  it.each(["long-report.png", "report.html"])(
    "opens %s from both canvas and message in the same closable file tab",
    async (name) => {
      vi.mocked(listWorkflowNodeResults).mockResolvedValueOnce({
        data: { items: [{ reference: ref, status: "succeeded" }], total: 1 },
        request: new Request("http://localhost/results"),
        response: new Response(),
      });
      vi.mocked(getWorkflowNodeResult).mockResolvedValueOnce({
        data: {
          present: true,
          reference: ref,
          fileView: { title: "文件产出" },
          result: name,
          files: [{ id: "file", field: "", name, available: true }],
        },
        request: new Request("http://localhost/result"),
        response: new Response(),
      });
      const scopeKey = `workflow-output:${name}`;
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const view = render(
        <QueryClientProvider client={client}>
          <WorkbenchProvider>
            <ResultOpenScope scopeKey={scopeKey}>
              <NodeOutputCard
                id={ref.workflowId}
                runId={ref.runId}
                nodeId="create_file"
                title="画布产出"
                files
              />
              <NodeResultPage reference={ref} compact />
            </ResultOpenScope>
            <OutputTabs scopeKey={scopeKey} />
          </WorkbenchProvider>
        </QueryClientProvider>,
      );
      await waitFor(() =>
        expect(screen.getByRole("button", { name: /画布产出/ })).toBeEnabled(),
      );
      fireEvent.click(screen.getByRole("button", { name: /画布产出/ }));
      const expected = [
        {
          reference: {
            type: "workflow-node-result",
            ids: {
              workflowId: ref.workflowId,
              runId: ref.runId,
              instanceId: ref.instanceId,
              attemptId: ref.attemptId,
            },
          },
          closeable: true,
        },
      ];
      expect(
        JSON.parse(screen.getByTestId("output-tabs").textContent ?? "[]"),
      ).toEqual(expected);
      fireEvent.click(await screen.findByRole("button", { name: /文件产出/ }));
      expect(
        JSON.parse(screen.getByTestId("output-tabs").textContent ?? "[]"),
      ).toEqual(expected);
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      view.unmount();
      client.clear();
      localStorage.removeItem(`ally.right-workbench.v2.${scopeKey}`);
    },
  );
  it.each([false, true])(
    "lets the authored Page own its heading in compact=%s",
    async (compact) => {
      vi.mocked(getWorkflowNodeResult).mockResolvedValueOnce({
        data: {
          present: true,
          reference: ref,
          page: {
            formatVersion: 1,
            catalogVersion: "workflow-result-v1",
            catalogDigest: catalogDigests["workflow-result-v1"],
            title: "工作台标签标题",
            description: "标签说明",
            spec: {
              root: "page",
              elements: {
                page: {
                  type: "Page",
                  props: { width: "readable" },
                  children: ["heading", "text"],
                },
                heading: {
                  type: "Heading",
                  props: { value: "Agent 编排的标题", level: "h1" },
                },
                text: { type: "Text", props: { value: { $state: "/result" } } },
              },
            },
          },
          result: "冻结产出",
          files: [],
        },
        request: new Request("http://localhost/result"),
        response: new Response(),
      });
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const view = render(
        <QueryClientProvider client={client}>
          <NodeResultPage reference={ref} compact={compact} />
        </QueryClientProvider>,
      );
      await screen.findByRole("heading", {
        name: "Agent 编排的标题",
        level: 1,
      });
      expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
      expect(screen.queryByText("工作台标签标题")).not.toBeInTheDocument();
      expect(screen.queryByText("标签说明")).not.toBeInTheDocument();
      expect(screen.getByText("冻结产出")).toBeInTheDocument();
      view.unmount();
      client.clear();
    },
  );
  it("work-board事件将后置产出关联到已结束的原消息，保留输入草稿且不污染续聊消息", async () => {
    const api = vi.mocked(getWorkflowChatResults);
    api.mockResolvedValue({
      data: [],
      request: new Request("http://localhost/results"),
      response: new Response(),
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={client}>
        <WorkbenchProvider>
          <ChatContributionsProvider
            value={{ Scope: WorkflowResultMessageScope }}
          >
            <ChatContributionScope
              chatId="worker-chat"
              scopeKey="chat:worker-chat"
              isRunning={false}
            >
              <MessageWindow />
            </ChatContributionScope>
          </ChatContributionsProvider>
        </WorkbenchProvider>
      </QueryClientProvider>,
    );
    fireEvent.change(screen.getByLabelText("草稿"), {
      target: { value: "keep me" },
    });
    await waitFor(() => expect(api).toHaveBeenCalled());
    api.mockResolvedValue({
      data: [ref],
      request: new Request("http://localhost/results"),
      response: new Response(),
    });
    act(() => {
      for (const listener of resourceListeners.get("work-board") ?? [])
        listener.onEvent({
          protocolVersion: 1,
          epoch: "epoch",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "work-board",
          type: "work-board.changed",
          resource: { kind: "chat", id: "worker-chat" },
          eventId: "late-effect",
          payload: {},
        });
    });
    await screen.findByText("冻结产出");
    expect(screen.getByTestId("original")).toHaveTextContent("冻结产出");
    expect(screen.getByTestId("continued")).toBeEmptyDOMElement();
    expect(screen.getByLabelText("草稿")).toHaveValue("keep me");
    view.unmount();
    client.clear();
  });
});

const resourceListeners = vi.hoisted(
  () => new Map<string, Set<GlobalEventObserver>>(),
);
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    const listeners =
      resourceListeners.get(topic) ?? new Set<GlobalEventObserver>();
    listeners.add(observer);
    resourceListeners.set(topic, listeners);
    return () => listeners.delete(observer);
  },
}));
