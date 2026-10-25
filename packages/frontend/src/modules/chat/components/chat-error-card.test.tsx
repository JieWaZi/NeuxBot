import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import {
  ChatErrorCard,
  ChatErrorNotices,
  useChatHistoryError,
} from "./chat-error-card";
import {
  dismissErrorNotices,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { renderHook } from "@testing-library/react";

afterEach(() => {
  const { result, unmount } = renderHook(useErrorNotices);
  act(() => dismissErrorNotices(result.current.map((notice) => notice.id)));
  unmount();
});

describe("消息错误卡片", () => {
  it("聊天历史加载遇到后端网络断连时不留下错误条", () => {
    const { result } = renderHook(() => {
      useChatHistoryError(
        {
          message: "Failed to fetch",
          error: new ApiError("Failed to fetch", "NETWORK"),
          onRetry: () => undefined,
        },
        "chat:one",
      );
      return useErrorNotices();
    });
    expect(result.current).toHaveLength(0);
  });

  it("历史查询成功后清除对应错误，保留同一聊天的其他错误", () => {
    const historyError = { message: "历史读取失败", onRetry: vi.fn() };
    const { result, rerender } = renderHook(
      ({ error }: { error: typeof historyError | undefined }) => {
        useChatHistoryError(error, "chat:history");
        return useErrorNotices();
      },
      {
        initialProps: {
          error: historyError as typeof historyError | undefined,
        },
      },
    );
    act(() =>
      reportErrorNotice({
        id: "other",
        scope: "chat:history",
        operation: "approval:one",
        message: "审批失败",
      }),
    );
    expect(result.current).toHaveLength(2);
    rerender({ error: undefined });
    expect(result.current.map((notice) => notice.id)).toEqual(["other"]);
  });

  it("恢复操作等待期间阻止连点，展示生命周期由所属操作维护", async () => {
    let resolve!: () => void;
    const retry = vi.fn(
      () =>
        new Promise<void>((done) => {
          resolve = done;
        }),
    );
    const { unmount } = render(
      <ChatErrorCard
        id="card-persist"
        message="模型服务暂时不可用"
        onRetry={retry}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(retry).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "重试" })).toBeDisabled();
    await act(async () => resolve());
    expect(screen.getByRole("alert")).toHaveTextContent("模型服务暂时不可用");
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    unmount();
  });

  it("历史执行失败默认收起，展开可查看原因且收起不删除记录", () => {
    const { container } = render(
      <ChatErrorCard
        id="execution:old"
        execution
        historical
        message="额度已用尽"
      />,
    );
    expect(screen.queryByText("额度已用尽")).not.toBeInTheDocument();
    expect(container.querySelector("[data-work-result-body]")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "展开失败详情" }));
    expect(screen.getByText("额度已用尽")).toBeInTheDocument();
    expect(container.querySelector("[data-work-result-body]")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "收起失败详情" }));
    expect(screen.queryByText("额度已用尽")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "展开失败详情" }),
    ).toBeInTheDocument();
  });

  it("关闭当前执行错误只收起，仍能展开查看", () => {
    render(
      <ChatErrorCard id="execution:current" execution message="执行失败原因" />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("执行失败原因");
    fireEvent.click(screen.getByRole("button", { name: "收起失败详情" }));
    fireEvent.click(screen.getByRole("button", { name: "展开失败详情" }));
    expect(screen.getByRole("alert")).toHaveTextContent("执行失败原因");
  });

  it("模型思考配置错误使用操作说明，完整协议错误保留在详情", () => {
    const raw = JSON.stringify({
      code: -32603,
      message: "Internal error",
      data: {
        details:
          "Current model does not support disabling thinking (modelId=step-5-preview)",
      },
    });
    render(<ChatErrorCard id="thinking" message={raw} />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "当前模型不支持关闭思考，请调整思考设置后重试。",
    );
    expect(screen.queryByText(raw)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "错误详情" }));
    expect(screen.getByText(raw)).toBeInTheDocument();
  });

  it("不可重试错误只有处理说明和关闭按钮", () => {
    render(<ChatErrorCard id="card-auth" message="请重新登录后发送" />);
    expect(
      screen.queryByRole("button", { name: "重试" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "关闭错误提示" }),
    ).toBeInTheDocument();
  });

  it("恢复操作位于卡片右下角，关闭按钮保留在右上角", () => {
    render(
      <ChatErrorCard
        id="card-check-position"
        message="发送结果未知"
        onRetry={() => undefined}
        actionLabel="检查状态"
      />,
    );
    const action = screen.getByRole("button", { name: "检查状态" });
    expect(action.closest("[data-chat-error-actions]")).toHaveClass(
      "justify-end",
    );
    expect(
      screen.getByRole("button", { name: "关闭错误提示" }),
    ).toBeInTheDocument();
  });

  it("结构化会话错误展示可读原因，原始 JSON 仅在详情展开后可见", () => {
    const raw = JSON.stringify({
      code: -32603,
      message: "Internal error",
      data: { error: "no rollout found for thread id abc" },
    });
    render(
      <ChatErrorCard
        id="card-codex-error"
        message={raw}
        onRetry={() => undefined}
        actionLabel="检查状态"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "no rollout found for thread id abc",
    );
    expect(screen.queryByText(raw)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "错误详情" }));
    expect(screen.getByText(raw)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "检查状态" }),
    ).not.toBeInTheDocument();
  });

  it("无法恢复的对话可选择在当前聊天继续", () => {
    const continueChat = vi.fn();
    render(
      <ChatErrorCard
        id="card-session-recovery"
        message="当前智能体无法继续这段对话"
        onContinue={continueChat}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "在这里继续" }));
    expect(continueChat).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole("button", { name: "重试" }),
    ).not.toBeInTheDocument();
  });

  it("重试接口失败留在消息卡片，并显示后端拒绝原因", async () => {
    render(
      <ChatErrorCard
        id="card-retry-failed"
        message="限流"
        onRetry={async () => {
          throw new ApiError("当前回复不能直接重试");
        }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText("当前回复不能直接重试")).toBeInTheDocument();
    expect(screen.getByText("限流")).toBeInTheDocument();
  });

  it("错误按对话隔离，切换回来仍保留，关闭后才删除", () => {
    reportErrorNotice({
      id: "scope-one",
      scope: "chat:one",
      message: "本对话发送失败",
    });
    reportErrorNotice({
      id: "scope-two",
      scope: "chat:two",
      message: "另一个对话发送失败",
    });
    const { rerender } = render(<ChatErrorNotices scopes={["chat:one"]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("本对话发送失败");
    rerender(<ChatErrorNotices scopes={["chat:two"]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("另一个对话发送失败");
    rerender(<ChatErrorNotices scopes={["chat:one"]} />);
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("默认只展示最新错误，手动展开后可分别重试和关闭", async () => {
    const retry = vi.fn();
    reportErrorNotice(
      {
        id: "older",
        scope: "chat:group",
        message: "较早错误",
        action: "retry",
      },
      retry,
    );
    reportErrorNotice({
      id: "latest",
      scope: "chat:group",
      message: "最新错误",
    });
    render(<ChatErrorNotices scopes={["chat:group"]} />);

    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.getByRole("alert")).toHaveTextContent("最新错误");
    expect(screen.queryByText("较早错误")).not.toBeInTheDocument();
    const expand = screen.getByRole("button", { name: "另外 1 条错误" });
    expect(expand).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(expand);
    expect(screen.getAllByRole("alert")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(retry).toHaveBeenCalledTimes(1));
    expect(screen.getAllByRole("alert")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "收起其他错误" }));
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "另外 1 条错误" }));
    fireEvent.click(screen.getByRole("button", { name: "关闭全部错误" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("关闭最新错误后自动展示上一条，关闭全部只影响当前对话", () => {
    reportErrorNotice({ id: "first", scope: "chat:one", message: "第一条" });
    reportErrorNotice({ id: "second", scope: "chat:one", message: "第二条" });
    reportErrorNotice({ id: "other", scope: "chat:two", message: "其他对话" });
    render(<ChatErrorNotices scopes={["chat:one"]} />);
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.getByRole("alert")).toHaveTextContent("第一条");
    expect(screen.queryByText("其他对话")).not.toBeInTheDocument();
  });
});
