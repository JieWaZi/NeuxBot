import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import "@/shared/i18n";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelTask as ChannelTask } from "@/shared/api/generated/types.gen";

import { TeamTaskBoard } from "./team-task-board";

function renderBoard(tasks: ChannelTask[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <TeamTaskBoard
        channelId="channel-1"
        members={[
          { id: "lead-1", name: "负责人" },
          { id: "member-1", name: "执行成员" },
        ]}
        onChanged={() => {}}
        tasks={tasks}
        threadId="thread-1"
      />
    </QueryClientProvider>,
  );
}

function taskCard(subject: string) {
  const card = screen
    .getByRole("heading", { name: subject })
    .closest("article");
  expect(card).not.toBeNull();
  if (!card) throw new Error(`任务 ${subject} 缺少卡片容器`);
  return card;
}

describe("TeamTaskBoard", () => {
  it("按状态展示任务，并用发起人和执行人表达流转关系", () => {
    renderBoard([
      {
        assigneeWakerId: "member-1",
        createdAt: 1_725_355_800_000,
        description: "补齐关键交互用例",
        id: "task-user",
        status: "pending",
        subject: "补充测试",
      },
      {
        assigneeWakerId: "member-1",
        createdAt: 1_725_355_860_000,
        description: "实现任务看板",
        id: "task-member",
        senderWakerId: "lead-1",
        status: "in_progress",
        subject: "实现功能",
      },
    ]);

    expect(screen.getByRole("region", { name: "看板" })).toBeVisible();
    for (const name of [
      "待执行",
      "进行中",
      "待人工验收",
      "已完成",
      "失败",
      "已取消",
    ]) {
      expect(screen.getByRole("heading", { name })).toBeVisible();
    }

    const userTask = taskCard("补充测试");
    expect(within(userTask).getByText("发起人")).toBeVisible();
    expect(within(userTask).getByText("用户")).toBeVisible();
    expect(within(userTask).getByText("执行人")).toBeVisible();
    expect(within(userTask).getByText("执行成员")).toBeVisible();
    expect(within(userTask).getByText("补齐关键交互用例")).toBeVisible();
    expect(within(userTask).getByRole("time")).not.toBeEmptyDOMElement();

    const memberTask = taskCard("实现功能");
    expect(within(memberTask).getByText("负责人")).toBeVisible();
    expect(within(memberTask).getByText("执行成员")).toBeVisible();
    expect(within(memberTask).queryByText("进行中")).not.toBeInTheDocument();
    expect(within(memberTask).queryByRole("img")).not.toBeInTheDocument();
    expect(memberTask).not.toHaveTextContent("负责人 → 执行成员");
  });

  it("长内容默认折叠并可在卡片内展开", () => {
    const description = "这是一段需要在任务卡片中保留的详细发送内容。".repeat(
      8,
    );
    renderBoard([
      {
        assigneeWakerId: "member-1",
        description,
        id: "task-long",
        status: "completed",
        subject: "整理长内容",
      },
    ]);
    const card = taskCard("整理长内容");
    const content = within(card).getByText(description);

    expect(content).toHaveClass("line-clamp-3");
    fireEvent.click(within(card).getByRole("button", { name: "展开内容" }));
    expect(content).not.toHaveClass("line-clamp-3");
    expect(
      within(card).getByRole("button", { name: "收起内容" }),
    ).toBeVisible();
  });
});
