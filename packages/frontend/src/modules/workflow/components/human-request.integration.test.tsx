import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { HumanRequestCard } from "./human-request";
import type { HumanRequest } from "../model/types";

const send = vi.hoisted(() =>
  vi.fn(async (input: unknown) => {
    void input;
    return {};
  }),
);
vi.mock("../api/workflow-queries", () => ({
  answerWorkflowHumanMutation: () => ({ mutationFn: send }),
  getWorkflowRunQueryKey: () => ["human-test"],
}));
afterEach(() => send.mockClear());
function question(options: HumanRequest["options"]) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <HumanRequestCard
        id="flow"
        runId="run"
        human={{
          id: "question",
          status: "waiting",
          prompt: "选择分析项目",
          questionVersion: 2,
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
        questionVersion: 2,
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
  await user.click(screen.getByRole("checkbox", { name: "选择分析项目" }));
  await user.click(screen.getByRole("button", { name: "提交答案" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({ body: expect.objectContaining({ value: true }) }),
    expect.anything(),
  );
});

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
