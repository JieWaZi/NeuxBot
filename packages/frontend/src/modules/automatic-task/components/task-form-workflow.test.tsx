import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, afterAll, expect, it, vi } from "vitest";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import "@/shared/i18n";
import { TaskFormDialog } from "./task-dialogs";
import { WorkflowTriggerDialog } from "./workflow-trigger-dialog";
import { MemoryRouter } from "react-router-dom";

vi.mock("@/shared/ui/execution-configuration-controls", () => ({
  ExecutionConfigurationControls: () => <span>执行配置控件</span>,
}));
vi.mock("@/modules/team", () => ({ TeamAvatarStack: () => null }));
vi.mock("./team-execution-controls", () => ({
  TeamExecutionControls: () => null,
}));

const { update, bulk } = vi.hoisted(() => ({
  update: vi.fn().mockResolvedValue({ data: { taskId: "task-a" } }),
  bulk: vi.fn(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  listWakers: async () => ({ data: { resources: [], totalSize: 0 } }),
  listChannels: async () => ({ data: { resources: [], totalSize: 0 } }),
  listRuntimes: async () => ({ data: { resources: [] } }),
  listAiAccounts: async () => ({ data: { resources: [] } }),
  listWorkflows: async () => ({
    data: {
      items: [{ id: "flow", title: "文本处理", currentRevisionId: "v1" }],
    },
  }),
  listWorkflowRevisions: async () => ({
    data: { items: [{ id: "v1", ordinal: 1 }] },
  }),
  getWorkflow: async () => ({
    data: {
      definition: { id: "flow", title: "文本处理" },
      revision: {
        id: "v1",
        manifest: {
          inputSchema: {
            type: "object",
            required: ["text"],
            properties: {
              text: { type: "string", title: "示例文本" },
              repeat: { type: "integer", default: 2 },
            },
          },
        },
      },
    },
  }),
  getWorkflowTriggers: async () => ({
    data: {
      fingerprint: "existing",
      tasks: [
        {
          id: "trigger-a",
          wakerId: "owner",
          trigger: "api",
          fast: false,
          workflowInputBindings: {
            text: { source: "api" },
            repeat: { source: "fixed", value: 5 },
          },
        },
      ],
    },
  }),
  updateWorkflowAutomaticTask: update,
  saveWorkflowTriggers: bulk,
}));

beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
afterAll(() => vi.unstubAllGlobals());

it("编辑工作流自动任务只保存当前任务，API 必填字段无需填写固定值", async () => {
  const user = userEvent.setup();
  const saved = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TaskFormDialog
        open
        onOpenChange={vi.fn()}
        onSaved={saved}
        task={{
          id: "task-a",
          wakerId: "owner",
          name: "测试任务",
          instruction: "",
          targetKind: "workflow",
          workflowId: "flow",
          workflowRevisionPolicy: "pinned",
          workflowRevisionId: "v1",
          workflowInputRevisionId: "v1",
          workflowInputBindings: {
            text: { source: "api" },
            repeat: { source: "fixed", value: 5 },
          },
          trigger: "api",
          enabled: true,
          revision: 3,
          fast: false,
        }}
      />
    </QueryClientProvider>,
  );
  await vi.dynamicImportSettled();
  await screen.findByLabelText("任务名称");
  await screen.findByText("调用时必须提供 args.text，无需在此填写。");
  await user.click(screen.getByText("版本设置"));
  expect(
    screen.getByRole("combobox", { name: "工作流版本" }),
  ).toBeInTheDocument();
  expect(screen.queryByText("执行配置控件")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("checkbox", { name: "只读工作区" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalled());
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    path: { taskId: "task-a" },
    body: {
      revision: 3,
      fast: false,
      workflowInputBindings: {
        text: { source: "api" },
        repeat: { source: "fixed", value: 5 },
      },
    },
  });
  expect(bulk).not.toHaveBeenCalled();
  expect(saved).toHaveBeenCalledWith({ taskId: "task-a" });
});

it("编辑工作流触发设置保留显式关闭的 Fast", async () => {
  bulk
    .mockClear()
    .mockResolvedValue({ data: { tasks: [{ taskId: "trigger-a" }] } });
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <WorkflowTriggerDialog
          definitionId="flow"
          open
          onOpenChange={vi.fn()}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  const save = await screen.findByRole("button", { name: "保存触发条件" });
  await waitFor(() => expect(save).toBeEnabled());
  await user.click(save);
  await waitFor(() => expect(bulk).toHaveBeenCalledTimes(1));
  expect(bulk.mock.calls[0]?.[0]).toMatchObject({
    body: { configuration: { fast: false } },
  });
});
