import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "i18next";
import { describe, expect, it, vi } from "vitest";

import {
  approvalOptionLabel,
  approvalQueuePosition,
  approvalResultLabel,
  ChatApprovalCard,
  matchApprovalOptionName,
  nextApprovalSeenIds,
} from "./chat-approval-card";
import "@/shared/i18n";

const approval = {
  approvalId: "approval-1",
  toolCallId: "tool-1",
  title: "连接互联网？",
  kind: "network",
  command: "curl https://chatgpt.com/",
  options: [
    { id: "reject-once", name: "Reject", kind: "reject_once" },
    { id: "allow-once", name: "Allow once", kind: "allow_once" },
    {
      id: "allow-session",
      name: "Allow for Session",
      kind: "allow_always",
    },
    {
      id: "allow-prefix",
      name: "Allow Commands Starting With `curl`",
      kind: "allow_always",
    },
  ],
};

describe("ChatApprovalCard", () => {
  it("翻译已知 ACP 选项并使用绿色允许操作", async () => {
    const user = userEvent.setup();
    const onResolve = vi.fn();
    const { container } = render(
      <ChatApprovalCard
        approval={approval}
        current={1}
        total={2}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );

    expect(screen.getByText("等待审批 · 1 / 2")).toBeInTheDocument();
    expect(screen.getByText("curl https://chatgpt.com/")).toBeInTheDocument();
    expect(container.querySelector(".lucide-globe")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /允许本次/ })).toHaveClass(
      "bg-success",
    );
    expect(screen.getByRole("button", { name: /允许本次/ })).not.toHaveClass(
      "rounded-none",
    );
    fireEvent.click(screen.getByRole("button", { name: /拒绝/ }));
    expect(onResolve).toHaveBeenCalledWith("approval-1", "reject-once");

    await user.click(screen.getByRole("button", { name: "更多审批选项" }));
    expect(await screen.findByText("本次会话允许")).toBeInTheDocument();
    expect(
      await screen.findByText("始终允许以 `curl` 开头的命令"),
    ).toBeInTheDocument();
  });

  it("卡片前台时支持 Enter 允许本次", () => {
    const onResolve = vi.fn();
    render(
      <ChatApprovalCard
        approval={approval}
        current={1}
        total={1}
        submissionState="idle"
        onResolve={onResolve}
      />,
    );

    fireEvent.keyDown(window, { key: "Enter" });
    expect(onResolve).toHaveBeenCalledWith("approval-1", "allow-once");
  });

  it("设置弹层打开时不响应 Enter 和 Esc", () => {
    const onResolve = vi.fn();
    render(
      <>
        <div role="dialog" data-state="open">
          设置
        </div>
        <ChatApprovalCard
          approval={approval}
          current={1}
          total={1}
          submissionState="idle"
          onResolve={onResolve}
        />
      </>,
    );

    fireEvent.keyDown(window, { key: "Enter" });
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onResolve).not.toHaveBeenCalled();
  });

  it("不会把 ACP 的持续拒绝伪装成主拒绝按钮", async () => {
    const user = userEvent.setup();
    render(
      <ChatApprovalCard
        approval={{
          ...approval,
          options: [
            { id: "allow-once", name: "Allow once", kind: "allow_once" },
            {
              id: "reject-always",
              name: "Always reject",
              kind: "reject_always",
            },
          ],
        }}
        current={1}
        total={1}
        submissionState="idle"
        onResolve={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: /拒绝/ })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "更多审批选项" }));
    expect(await screen.findByText("始终拒绝")).toBeInTheDocument();
  });

  it("显示真实审批序号，未知选项保留 Agent 原名", async () => {
    const user = userEvent.setup();
    render(
      <ChatApprovalCard
        approval={{
          ...approval,
          options: [
            { id: "reject-once", name: "Reject", kind: "reject_once" },
            { id: "allow-once", name: "Allow once", kind: "allow_once" },
            {
              id: "custom-scope",
              name: "Allow this repo clone",
              kind: "allow_repo",
            },
          ],
        }}
        current={2}
        total={3}
        submissionState="idle"
        onResolve={vi.fn()}
      />,
    );

    expect(screen.getByText("等待审批 · 2 / 3")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "更多审批选项" }));
    expect(
      await screen.findByText("Allow this repo clone"),
    ).toBeInTheDocument();
  });

  it("已处理的审批仍计入序号", () => {
    const first = nextApprovalSeenIds(
      [{ approvalId: "a" }, { approvalId: "b" }],
      [],
    );
    expect(first).toEqual(["a", "b"]);
    expect(approvalQueuePosition("a", first)).toEqual({ current: 1, total: 2 });
    const remaining = nextApprovalSeenIds([{ approvalId: "b" }], first);
    expect(remaining).toEqual(["a", "b"]);
    expect(approvalQueuePosition("b", remaining)).toEqual({
      current: 2,
      total: 2,
    });
  });

  it("同一 ACP 英文选项只匹配一次，选择时和已处理用两套文案", () => {
    const t = i18n.getFixedT("zh-CN", "chat");
    expect(matchApprovalOptionName("Allow for Session")).toEqual({
      type: "named",
      key: "allowSession",
    });
    expect(
      matchApprovalOptionName("Allow Commands Starting With `curl`"),
    ).toEqual({
      type: "allowCommandPrefix",
      command: "curl",
    });
    expect(approvalOptionLabel({ name: "Allow for Session" }, t)).toBe(
      "本次会话允许",
    );
    expect(
      approvalResultLabel({ name: "Allow for Session", result: "selected" }, t),
    ).toBe("本次会话已允许");
    expect(
      approvalOptionLabel({ name: "Allow Commands Starting With `curl`" }, t),
    ).toBe("始终允许以 `curl` 开头的命令");
    expect(
      approvalResultLabel(
        {
          name: "Allow Commands Starting With `curl`",
          result: "selected",
        },
        t,
      ),
    ).toBe("已允许以 `curl` 开头的命令");
  });
});
