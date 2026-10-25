import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as Team } from "@/shared/api/generated/types.gen";
import { TeamExecutionSummary } from "./team-execution-summary";
import "@/shared/i18n";

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: ({ value }: { value: Record<string, string> }) => ({
    runtime: { id: "codex", name: "Codex" },
    accounts: [],
    models: [
      { id: "team-model", name: "团队模型" },
      { id: "task-model", name: "任务模型" },
    ],
    reasoningOptions: [{ id: "high", name: "High" }],
    selection: { ...value, accountId: "system" },
  }),
}));

const team = {
  channel: { leadWakerId: "member-1" },
  members: [
    {
      waker: { id: "member-1", name: "资料助手" },
      runtimeId: "codex",
      model: "team-model",
      permissionLevel: "default",
    },
  ],
} satisfies Team;

it("未自定义时处理服务端空覆盖并展示团队配置", () => {
  render(<TeamExecutionSummary team={team} overrides={null} />);
  expect(screen.getByText("团队模型")).toBeVisible();
  expect(screen.queryByText("已自定义")).not.toBeInTheDocument();
});

it("详情展示任务成员覆盖而不替换团队默认配置", () => {
  render(
    <TeamExecutionSummary
      team={team}
      overrides={[
        {
          wakerId: "member-1",
          runtimeId: "codex",
          model: "task-model",
          reasoning: "high",
          permissionLevel: "default",
        },
      ]}
    />,
  );
  expect(screen.getByText("任务模型")).toBeVisible();
  expect(screen.getByText("High")).toBeVisible();
  expect(screen.getByText("已自定义")).toBeVisible();
  expect(team.members[0]?.model).toBe("team-model");
});
