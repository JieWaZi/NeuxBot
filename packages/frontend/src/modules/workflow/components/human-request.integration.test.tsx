import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import {
  getWorkflowRunQueryKey,
  listChatWorkflowRunsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListChatWorkflowRunsResponse } from "@/shared/api/generated/types.gen";
import { HumanRequestCard, WorkflowChatQuestion } from "./human-request";
import type { HumanRequest } from "../model/types";
import { WorkflowAnswerDraftProvider } from "../model/human-answer-draft";

const send = vi.hoisted(() =>
  vi.fn(async (input: unknown) => {
    void input;
    return {};
  }),
);
vi.mock(import("../api/workflow-queries"), async (importOriginal) => ({
  ...(await importOriginal()),
  answerWorkflowHumanMutation: () => ({ mutationFn: send }),
}));
afterEach(() => send.mockClear());
function question(
  options: HumanRequest["options"],
  presentation: "canvas" | "chat" = "canvas",
) {
  const Question =
    presentation === "chat" ? WorkflowChatQuestion : HumanRequestCard;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Question
        id="flow"
        runId="run"
        human={{
          id: "question",
          status: "waiting",
          prompt: "选择分析项目",
          snapshotDigest: "frozen",
          options,
        }}
      />
    </QueryClientProvider>,
  );
}
it("动态多选限制数量并携带冻结版本与摘要", async () => {
  const user = userEvent.setup();
  question({
    kind: "multiselect",
    choices: [
      { value: "a", label: "候选甲" },
      { value: "b", label: "候选乙" },
    ],
    minSelections: 1,
    maxSelections: 1,
  });
  expect(screen.getByRole("button", { name: "提交答案" })).toBeDisabled();
  await user.click(screen.getByRole("checkbox", { name: "候选甲" }));
  await user.click(screen.getByRole("checkbox", { name: "候选乙" }));
  expect(screen.getByRole("button", { name: "提交答案" })).toBeDisabled();
  await user.click(screen.getByRole("checkbox", { name: "候选乙" }));
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({
        value: ["a"],
        skip: false,
        snapshotDigest: "frozen",
      }),
    }),
    expect.anything(),
  );
});
it("受限单选不出现自由输入，跳过使用独立命令", async () => {
  const user = userEvent.setup();
  question({
    kind: "select",
    choices: [{ value: "a", label: "候选甲" }],
    allowCustom: false,
    allowSkip: true,
  });
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "跳过" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({ body: expect.objectContaining({ skip: true }) }),
    expect.anything(),
  );
});
it("确认问题必须由用户主动选择", async () => {
  const user = userEvent.setup();
  question({ kind: "confirm" });
  expect(screen.getByRole("button", { name: "提交答案" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "是" }));
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({ body: expect.objectContaining({ value: true }) }),
    expect.anything(),
  );
});

it("聊天中已完成的工作流问题在题目旁显示已回答，正文回显答案", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowChatQuestion
        id="flow"
        runId="run"
        human={{
          id: "question",
          status: "answered",
          source: "user",
          prompt: "新版是否确认？",
          options: { kind: "confirm" },
          value: true,
        }}
      />
    </QueryClientProvider>,
  );
  const card = within(screen.getByRole("article"));
  expect(card.getByText("已回答").parentElement).toHaveTextContent(
    "新版是否确认？",
  );
  expect(card.getByText("是")).toBeVisible();
  expect(card.queryByText("用户回答")).not.toBeInTheDocument();
});

it("画布与聊天双向同步草稿，同名运行互不影响，从任一侧只能提交一次", async () => {
  const user = userEvent.setup();
  const human: HumanRequest = {
    id: "question",
    status: "waiting",
    prompt: "核对通过？",
    snapshotDigest: "v1",
    options: { kind: "confirm" },
  };
  let resolve!: () => void;
  send.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = () => done({});
      }),
  );
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowAnswerDraftProvider>
        <section aria-label="聊天">
          <WorkflowChatQuestion id="flow" runId="one" human={human} />
        </section>
        <section aria-label="画布">
          <HumanRequestCard id="flow" runId="one" human={human} />
        </section>
        <section aria-label="另一次">
          <HumanRequestCard id="flow" runId="two" human={human} />
        </section>
        <section aria-label="新版问题">
          <HumanRequestCard
            id="flow"
            runId="one"
            human={{ ...human, snapshotDigest: "v2" }}
          />
        </section>
      </WorkflowAnswerDraftProvider>
    </QueryClientProvider>,
  );
  const chat = within(screen.getByRole("region", { name: "聊天" }));
  const canvas = within(screen.getByRole("region", { name: "画布" }));
  await user.click(canvas.getByRole("button", { name: "否" }));
  expect(
    chat.getByRole("button", { name: "否", pressed: true }),
  ).toBeInTheDocument();
  await user.click(chat.getByRole("button", { name: "是" }));
  expect(
    canvas.getByRole("button", { name: "是", pressed: true }),
  ).toBeInTheDocument();
  for (const name of ["另一次", "新版问题"]) {
    const other = within(screen.getByRole("region", { name }));
    expect(other.getByRole("button", { name: "提交答案" })).toBeDisabled();
  }
  expect(send).not.toHaveBeenCalled();
  await user.click(canvas.getByRole("button", { name: "提交答案" }));
  expect(chat.getByRole("button", { name: "继续" })).toBeDisabled();
  await user.click(chat.getByRole("button", { name: "继续" }));
  await act(async () => resolve());
  expect(send).toHaveBeenCalledTimes(1);
  expect(chat.getByRole("button", { name: "继续" })).toBeDisabled();
});

it("关闭所有视图仍锁定在途答案，失败后保留原答案和重试身份", async () => {
  const user = userEvent.setup();
  const human: HumanRequest = {
    id: "question",
    status: "waiting",
    snapshotDigest: "frozen",
    options: { kind: "text", defaultValue: "默认答案" },
  };
  let reject!: (error: ApiError) => void;
  send.mockImplementationOnce(
    () =>
      new Promise((_resolve, fail) => {
        reject = fail;
      }),
  );
  const client = new QueryClient();
  const view = (surface?: "canvas" | "chat") => (
    <QueryClientProvider client={client}>
      <WorkflowAnswerDraftProvider>
        {surface === "canvas" ? (
          <HumanRequestCard id="flow" runId="run" human={human} />
        ) : surface === "chat" ? (
          <WorkflowChatQuestion id="flow" runId="run" human={human} />
        ) : null}
      </WorkflowAnswerDraftProvider>
    </QueryClientProvider>
  );
  const { rerender } = render(view("canvas"));
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "保留这份答案" },
  });
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  expect(send).toHaveBeenCalledOnce();

  rerender(view());
  rerender(view("chat"));
  const pendingAction = screen.getByRole("button", { name: "继续" });
  expect(pendingAction).toBeDisabled();
  await user.click(pendingAction);
  expect(send).toHaveBeenCalledOnce();
  expect(screen.getByRole("textbox")).toHaveValue("保留这份答案");

  rerender(view());
  await act(async () => reject(new ApiError("提交暂时失败")));
  rerender(view("chat"));
  expect(screen.getByRole("alert")).toHaveTextContent("提交暂时失败");
  expect(screen.getByRole("textbox")).toHaveValue("保留这份答案");
  await user.click(screen.getByRole("button", { name: "继续" }));
  expect(send).toHaveBeenCalledTimes(2);
  expect(send.mock.calls[1]?.[0]).toEqual(send.mock.calls[0]?.[0]);
});

it("在画布输入和在聊天清空修改的是同一个文本答案", async () => {
  const user = userEvent.setup();
  const human: HumanRequest = {
    id: "text-question",
    status: "waiting",
    prompt: "填写备注",
    snapshotDigest: "frozen",
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowAnswerDraftProvider>
        <section aria-label="聊天">
          <WorkflowChatQuestion id="flow" runId="run" human={human} />
        </section>
        <section aria-label="画布">
          <HumanRequestCard id="flow" runId="run" human={human} />
        </section>
      </WorkflowAnswerDraftProvider>
    </QueryClientProvider>,
  );
  const chat = within(screen.getByRole("region", { name: "聊天" }));
  const canvas = within(screen.getByRole("region", { name: "画布" }));
  await user.type(canvas.getByRole("textbox"), "核对后归档");
  expect(chat.getByRole("textbox")).toHaveValue("核对后归档");
  await user.clear(chat.getByRole("textbox"));
  expect(canvas.getByRole("textbox")).toHaveValue("");
  expect(canvas.getByRole("button", { name: "提交答案" })).toBeDisabled();
  expect(send).not.toHaveBeenCalled();
});

it("聊天与画布同步未解析 JSON 和输入结构，不能提交旧答案", async () => {
  const user = userEvent.setup();
  const human: HumanRequest = {
    id: "schema-question",
    status: "waiting",
    snapshotDigest: "frozen",
    options: {
      kind: "form",
      schema: {
        title: "评审配置",
        oneOf: [
          {
            title: "分支配置",
            type: "object",
            required: ["branch"],
            properties: { branch: { type: "string", title: "分支" } },
          },
          {
            title: "版本配置",
            type: "object",
            required: ["revision"],
            properties: { revision: { type: "string", title: "版本" } },
          },
        ],
      },
    },
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkflowAnswerDraftProvider>
        <section aria-label="聊天">
          <WorkflowChatQuestion id="flow" runId="run" human={human} />
        </section>
        <section aria-label="画布">
          <HumanRequestCard id="flow" runId="run" human={human} />
        </section>
      </WorkflowAnswerDraftProvider>
    </QueryClientProvider>,
  );
  const chat = within(screen.getByRole("region", { name: "聊天" }));
  const canvas = within(screen.getByRole("region", { name: "画布" }));
  const raw = '{"branch":';
  fireEvent.change(canvas.getByRole("textbox", { name: "评审配置" }), {
    target: { value: raw },
  });
  const chatInput = chat.getByRole("textbox", { name: "评审配置" });
  expect(chatInput).toHaveValue(raw);
  expect(chat.getByRole("alert")).toHaveTextContent("请输入有效 JSON");
  await user.click(chat.getByRole("button", { name: "继续" }));
  expect(chat.getByRole("button", { name: "继续" })).toBeDisabled();
  expect(canvas.getByRole("button", { name: "提交答案" })).toBeDisabled();
  expect(send).not.toHaveBeenCalled();

  const repaired = '{"branch":"main"}';
  fireEvent.change(chatInput, { target: { value: repaired } });
  expect(canvas.getByRole("textbox", { name: "评审配置" })).toHaveValue(
    repaired,
  );
  expect(canvas.getByRole("button", { name: "提交答案" })).toBeEnabled();
  await user.click(chat.getByRole("combobox", { name: "输入结构" }));
  await user.click(await screen.findByRole("option", { name: "分支配置" }));
  const branch = canvas.getByRole("textbox", { name: "分支" });
  expect(branch).toHaveValue("main");
  await user.clear(branch);
  await user.type(branch, "release");
  expect(chat.getByRole("textbox", { name: "分支" })).toHaveValue("release");
  await user.click(chat.getByRole("button", { name: "继续" }));
  expect(send).toHaveBeenCalledOnce();
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({ value: { branch: "release" } }),
    }),
    expect.anything(),
  );
});

it.each(["独立画布", "聊天工作台"])(
  "%s遇到运行状态冲突会精确补读运行和源聊天，并锁定旧问题",
  async (presentation) => {
    const user = userEvent.setup();
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const run = getWorkflowRunQueryKey({
      path: { id: "flow", runId: "run" },
      query: { offset: 20 },
    });
    const summary = listChatWorkflowRunsQueryKey({
      path: { chatId: "chat" },
      query: { executionIds: "execution" },
    });
    const unrelated = getWorkflowRunQueryKey({
      path: { id: "flow", runId: "other" },
    });
    const otherChat = listChatWorkflowRunsQueryKey({
      path: { chatId: "other-chat" },
      query: { executionIds: "another-execution" },
    });
    let publish!: (human: HumanRequest) => void;
    const next = new Promise<HumanRequest>((resolve) => {
      publish = resolve;
    });
    const human: HumanRequest = {
      id: "choice",
      status: "waiting",
      prompt: "发布评审？",
      snapshotDigest: "frozen",
      options: { kind: "confirm" },
    };
    const readRun = vi
      .fn(async () => human)
      .mockImplementationOnce(async () => human)
      .mockImplementationOnce(() => next);
    const summaryOf = (human: HumanRequest): ListChatWorkflowRunsResponse => ({
      resources: [
        {
          id: "parent-run",
          definitionId: "parent-flow",
          executionId: "execution",
          [human.status === "waiting" ? "activeRequests" : "answeredRequests"]:
            [
              {
                kind: "human",
                definitionId: "flow",
                runId: "run",
                requestId: "choice",
                human,
              },
            ],
        },
      ],
    });
    const readSummary = vi
      .fn(async () => summaryOf(human))
      .mockImplementationOnce(async () => summaryOf(human))
      .mockImplementationOnce(() => next.then(summaryOf));
    const readOther = vi.fn(async () => human);
    const readOtherChat = vi.fn(async () => ({ resources: [] }));
    const onAnswered = vi.fn();
    send.mockRejectedValueOnce(
      new ApiError(
        "请求已处理",
        presentation === "独立画布" ? "40900003003" : "WorkflowStateConflict",
        409,
      ),
    );
    function BothViews() {
      const currentRun = useQuery({ queryKey: run, queryFn: readRun });
      const currentSummary = useQuery({
        queryKey: summary,
        queryFn: readSummary,
      });
      useQuery({ queryKey: unrelated, queryFn: readOther });
      useQuery({ queryKey: otherChat, queryFn: readOtherChat });
      const record = currentSummary.data?.resources?.[0];
      const chatHuman =
        record?.activeRequests?.[0]?.human ??
        record?.answeredRequests?.[0]?.human;
      return (
        <WorkflowAnswerDraftProvider>
          <section aria-label="聊天">
            {chatHuman && (
              <WorkflowChatQuestion
                id="flow"
                runId="run"
                chatId="chat"
                human={chatHuman}
              />
            )}
          </section>
          <section aria-label="画布">
            {currentRun.data && (
              <HumanRequestCard
                id="flow"
                runId="run"
                chatId={presentation === "聊天工作台" ? "chat" : undefined}
                human={currentRun.data}
                onAnswered={onAnswered}
              />
            )}
          </section>
        </WorkflowAnswerDraftProvider>
      );
    }
    render(
      <QueryClientProvider client={client}>
        <BothViews />
      </QueryClientProvider>,
    );
    const chat = within(screen.getByRole("region", { name: "聊天" }));
    const canvas = within(screen.getByRole("region", { name: "画布" }));
    await chat.findByRole("button", { name: "继续" });
    await user.click(await canvas.findByRole("button", { name: "是" }));
    await user.click(canvas.getByRole("button", { name: "提交答案" }));
    await waitFor(() => {
      expect(readRun).toHaveBeenCalledTimes(2);
      expect(readSummary).toHaveBeenCalledTimes(2);
    });
    expect(readOther).toHaveBeenCalledTimes(1);
    expect(readOtherChat).toHaveBeenCalledTimes(1);
    expect(canvas.getByRole("button", { name: "提交答案" })).toBeDisabled();
    expect(chat.getByRole("button", { name: "继续" })).toBeDisabled();
    await user.click(chat.getByRole("button", { name: "继续" }));
    expect(send).toHaveBeenCalledOnce();
    expect(onAnswered).not.toHaveBeenCalled();
    await act(async () =>
      publish({ ...human, status: "answered", source: "user", value: false }),
    );
    await waitFor(() =>
      expect(
        chat.queryByRole("button", { name: "继续" }),
      ).not.toBeInTheDocument(),
    );
    expect(
      canvas.queryByRole("button", { name: "提交答案" }),
    ).not.toBeInTheDocument();
    expect(canvas.queryByRole("alert")).not.toBeInTheDocument();
    expect(chat.queryByRole("alert")).not.toBeInTheDocument();
  },
);

it("搜索和翻页只读冻结候选，已选值跨页保留", async () => {
  const user = userEvent.setup();
  question({
    kind: "multiselect",
    choices: Array.from({ length: 25 }, (_, i) => ({
      value: String(i),
      label: `候选${i}`,
    })),
  });
  await user.click(screen.getByRole("checkbox", { name: "候选0" }));
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(screen.getByRole("checkbox", { name: "候选24" })).not.toBeChecked();
  await user.type(screen.getByRole("textbox", { name: "搜索候选" }), "候选24");
  await user.click(screen.getByRole("checkbox", { name: "候选24" }));
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({ value: ["0", "24"] }),
    }),
    expect.anything(),
  );
});

it("聊天提问卡用布尔值提交否，保留原运行与题目身份", async () => {
  const user = userEvent.setup();
  question({ kind: "confirm" }, "chat");
  const next = screen.getByRole("button", { name: "继续" });
  expect(next).toBeDisabled();
  await user.click(screen.getByRole("button", { name: /否/ }));
  await user.click(next);
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { id: "flow", runId: "run", humanId: "question" },
      body: expect.objectContaining({
        value: false,
        skip: false,
        snapshotDigest: "frozen",
      }),
    }),
    expect.anything(),
  );
});

it("聊天多选使用已有选项卡并保留稳定值与数量限制", async () => {
  const user = userEvent.setup();
  question(
    {
      kind: "multiselect",
      choices: [
        { label: "分析", value: "analyze" },
        { label: "总结", value: "summary" },
      ],
      minSelections: 2,
      maxSelections: 2,
    },
    "chat",
  );
  const next = screen.getByRole("button", { name: "继续" });
  await user.click(screen.getByRole("button", { name: /分析$/ }));
  expect(next).toBeDisabled();
  await user.click(screen.getByRole("button", { name: /总结$/ }));
  await user.click(next);
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({ value: ["analyze", "summary"] }),
    }),
    expect.anything(),
  );
});
