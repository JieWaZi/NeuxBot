import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { useSceneWorkspace, WorkspaceProvider } from "./workspace-context";

function Harness({ scopeKey }: { scopeKey: string }) {
  const workspace = useSceneWorkspace(scopeKey);
  const { snapshot } = workspace;
  return (
    <div>
      <output data-testid={`${scopeKey}-tabs`}>
        {snapshot.tabs.map((tab) => tab.label).join(",")}
      </output>
      <output data-testid={`${scopeKey}-active`}>{snapshot.activeTabId}</output>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({
            type: "open-member",
            member: { chatId: "chat-1", label: "成员甲", wakerId: "member-1" },
          })
        }
      >
        {scopeKey} 打开成员甲
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({
            type: "open-member",
            member: { chatId: "chat-2", label: "成员乙", wakerId: "member-2" },
          })
        }
      >
        {scopeKey} 打开成员乙
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({
            type: "reorder-tab",
            sourceTabId: `member:${scopeKey}:member-2`,
            targetTabId: `member:${scopeKey}:member-1`,
          })
        }
      >
        {scopeKey} 重排成员
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({
            type: "close-tab",
            tabId: `member:${scopeKey}:member-2`,
          })
        }
      >
        {scopeKey} 关闭成员乙
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({ type: "select-tab", tabId: "missing" })
        }
      >
        {scopeKey} 选择失效标签
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({ type: "migrate", toScopeKey: "created" })
        }
      >
        {scopeKey} 迁移
      </button>
      <button
        type="button"
        onClick={() =>
          workspace.dispatch({ type: "clear-prefix", scopePrefix: "team:" })
        }
      >
        {scopeKey} 清理团队
      </button>
    </div>
  );
}

describe("Scene 主标签状态", () => {
  it("成员在范围内去重，成员重排后关闭活动标签回到对话", async () => {
    const user = userEvent.setup();
    render(
      <WorkspaceProvider>
        <Harness scopeKey="team:one" />
        <Harness scopeKey="team:two" />
      </WorkspaceProvider>,
    );
    await user.click(
      screen.getByRole("button", { name: "team:one 打开成员甲" }),
    );
    await user.click(
      screen.getByRole("button", { name: "team:one 打开成员乙" }),
    );
    await user.click(
      screen.getByRole("button", { name: "team:one 打开成员乙" }),
    );
    await user.click(screen.getByRole("button", { name: "team:one 重排成员" }));
    expect(screen.getByTestId("team:one-tabs")).toHaveTextContent(
      "对话,成员乙,成员甲",
    );
    expect(screen.getByTestId("team:two-tabs")).toHaveTextContent("对话");
    await user.click(
      screen.getByRole("button", { name: "team:one 关闭成员乙" }),
    );
    expect(screen.getByTestId("team:one-active")).toHaveTextContent(
      "chat:team:one",
    );
  });

  it("迁移活动成员时重写范围身份，失效选择不制造悬空活动标签", async () => {
    const user = userEvent.setup();
    render(
      <WorkspaceProvider>
        <Harness scopeKey="draft" />
        <Harness scopeKey="created" />
      </WorkspaceProvider>,
    );
    await user.click(screen.getByRole("button", { name: "draft 打开成员甲" }));
    await user.click(
      screen.getByRole("button", { name: "draft 选择失效标签" }),
    );
    expect(screen.getByTestId("draft-active")).toHaveTextContent(
      "member:draft:member-1",
    );
    await user.click(screen.getByRole("button", { name: "draft 迁移" }));
    expect(screen.getByTestId("draft-tabs")).toHaveTextContent("对话");
    expect(screen.getByTestId("created-tabs")).toHaveTextContent("对话,成员甲");
    expect(screen.getByTestId("created-active")).toHaveTextContent(
      "member:created:member-1",
    );
  });

  it("删除团队时按前缀清理所有团队范围，不影响普通对话", async () => {
    const user = userEvent.setup();
    render(
      <WorkspaceProvider>
        <Harness scopeKey="team:one" />
        <Harness scopeKey="team:two" />
        <Harness scopeKey="partner:one" />
      </WorkspaceProvider>,
    );
    for (const scopeKey of ["team:one", "team:two", "partner:one"]) {
      await user.click(
        screen.getByRole("button", { name: `${scopeKey} 打开成员甲` }),
      );
    }
    await user.click(screen.getByRole("button", { name: "team:one 清理团队" }));
    expect(screen.getByTestId("team:one-tabs")).toHaveTextContent("对话");
    expect(screen.getByTestId("team:two-tabs")).toHaveTextContent("对话");
    expect(screen.getByTestId("partner:one-tabs")).toHaveTextContent(
      "对话,成员甲",
    );
  });
});
