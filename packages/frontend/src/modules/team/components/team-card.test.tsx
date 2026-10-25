import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail } from "@/shared/api/generated/types.gen";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import { TeamCard } from "./team-card";

const fixture = vi.hoisted(() => ({
  remove: vi.fn(),
}));

vi.mock("../api/channel-queries", () => ({
  deleteChannelMutation: () => ({ mutationFn: fixture.remove }),
  teamQueryKeys: { channels: () => ["channels"] },
}));

vi.mock("./team-form-dialog", () => ({
  TeamFormDialog: ({ open }: { open: boolean }) =>
    open ? (
      <div role="dialog" aria-label="编辑团队">
        编辑团队
      </div>
    ) : null,
}));

function WorkspaceProbe() {
  const workspace = useSceneWorkspace("team:team-1:thread-1");
  return (
    <>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({
            type: "open-member",
            member: { label: "成员甲", wakerId: "member-1" },
          })
        }
      >
        打开团队成员标签
      </button>
      <output aria-label="团队工作区状态">
        {workspace.snapshot.tabs.map((tab) => tab.label).join(",")}
      </output>
    </>
  );
}

function renderCard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const detail = {
    channel: {
      id: "team-1",
      leadWakerId: "lead-1",
      name: "研发团队",
    },
    members: [{ waker: { id: "lead-1", name: "负责人" } }],
  } as ChannelDetail;
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceProvider>
        <MemoryRouter>
          <TeamCard detail={detail} />
          <WorkspaceProbe />
        </MemoryRouter>
      </WorkspaceProvider>
    </QueryClientProvider>,
  );
}

describe("TeamCard", () => {
  beforeEach(() => {
    fixture.remove.mockReset();
  });

  it("点击卡片主体打开编辑弹窗，而不是跳转聊天", async () => {
    renderCard();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "编辑 研发团队" }));

    expect(
      await screen.findByRole("dialog", { name: "编辑团队" }),
    ).toBeInTheDocument();
  });

  it("删除失败时在确认对话框展示错误", async () => {
    fixture.remove.mockRejectedValue(new Error("failed"));
    renderCard();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "研发团队更多操作" }));
    await user.click(await screen.findByRole("menuitem", { name: "删除团队" }));
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "删除失败。若团队仍在工作，请先停止当前工作后重试。",
    );
  });

  it("确认删除时调用团队永久删除接口", async () => {
    fixture.remove.mockResolvedValue({});
    renderCard();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "研发团队更多操作" }));
    await user.click(await screen.findByRole("menuitem", { name: "删除团队" }));
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    expect(fixture.remove.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { channelId: "team-1" },
      }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("团队工作区状态")).toHaveTextContent(
        /^对话$/,
      ),
    );
  });

  it("删除团队时按 Channel 前缀清理全部会话工作区", async () => {
    fixture.remove.mockResolvedValue({});
    renderCard();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "打开团队成员标签" }));
    expect(screen.getByLabelText("团队工作区状态")).toHaveTextContent("成员甲");
    await user.click(screen.getByRole("button", { name: "研发团队更多操作" }));
    await user.click(await screen.findByRole("menuitem", { name: "删除团队" }));
    await user.click(screen.getByRole("button", { name: "确认删除" }));

    await waitFor(() =>
      expect(screen.getByLabelText("团队工作区状态")).toHaveTextContent(
        /^对话$/,
      ),
    );
  });
});
