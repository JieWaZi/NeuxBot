import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import { classifyChatSendFailure } from "../runtime/chat-send-failure";
import {
  ChatErrorActions,
  ChatErrorCard,
  MessageFailure,
  ChatErrorNotices,
  useChatHistoryError,
} from "./chat-error-card";
import {
  dismissErrorNotices,
  reportErrorNotice,
  reportChatError,
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
        messageSource: "public",
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

  it("模型思考配置错误展示原生原因，完整协议错误保留在详情", () => {
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
      "Current model does not support disabling thinking (modelId=step-5-preview)",
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
    expect(screen.queryByText("限流")).not.toBeInTheDocument();
  });

  it("重试返回同一原生原因时不重复显示", async () => {
    render(
      <ChatErrorCard
        id="same-retry"
        message="Native quota reason"
        onRetry={async () => {
          throw new ApiError("Native quota reason");
        }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "重试" })).not.toBeDisabled(),
    );
    expect(screen.getAllByText("Native quota reason")).toHaveLength(1);
  });

  it("错误按对话隔离，切换回来仍保留，关闭后才删除", () => {
    reportErrorNotice({
      messageSource: "public",
      id: "scope-one",
      scope: "chat:one",
      message: "本对话发送失败",
    });
    reportErrorNotice({
      messageSource: "public",
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

  it("同一失败同时来自消息和提示时关闭一次，切换聊天后也不重复弹出", () => {
    const failure = {
      id: "failed-one",
      role: "assistant" as const,
      status: "failed" as const,
      errorCode: "QUOTA_EXHAUSTED",
    };
    reportErrorNotice({
      messageSource: "public",
      id: "duplicate",
      scope: "chat:one",
      message: "当前额度已用尽。请切换模型，或等待额度恢复后重试。",
    });
    const { rerender } = render(
      <ChatErrorNotices scopes={["chat:one"]} failure={failure} />,
    );
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    rerender(<ChatErrorNotices scopes={["chat:two"]} />);
    rerender(<ChatErrorNotices scopes={["chat:one"]} failure={failure} />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("同一原生额度原因只展示最新一条，关闭后不复现", () => {
    reportChatError(
      new ApiError(
        "当前额度已用尽，请切换模型或等待额度恢复后重试",
        "RuntimeQuotaExhausted",
      ),
      "chat:quota",
      "发送失败",
    );
    reportErrorNotice({
      messageSource: "public",
      id: "quota-other",
      scope: "chat:quota",
      message: "另一个操作失败",
    });
    render(
      <ChatErrorNotices
        scopes={["chat:quota"]}
        failure={{
          id: "quota-failed",
          role: "assistant",
          status: "failed",
          errorCode: "QUOTA_EXHAUSTED",
          errorMessage: "当前额度已用尽，请切换模型或等待额度恢复后重试",
        }}
      />,
    );
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: /另外/ }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("关闭执行失败后再次出现的原生错误仍会展示", () => {
    render(
      <ChatErrorNotices
        scopes={["chat:repeat"]}
        failure={{
          id: "failed-repeat",
          role: "assistant",
          status: "failed",
          errorMessage: "Native quota reason",
        }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "new-attempt",
        scope: "chat:repeat",
        message: "Native quota reason",
      }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Native quota reason");
  });

  it("会话缺失的原生原因经过发送接口后仍直接展示", () => {
    const failure = classifyChatSendFailure(
      new ApiError(
        "Native session missing: choose a different session",
        "RuntimeSessionNotFound",
      ),
    );
    render(<ChatErrorNotices scopes={[]} fallback={failure} />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Native session missing: choose a different session",
    );
  });

  it("新的执行失败不会被此前其他操作的错误遮住", () => {
    reportErrorNotice({
      messageSource: "public",
      id: "old-attachment",
      scope: "chat:latest",
      message: "附件读取失败",
    });
    render(
      <ChatErrorNotices
        scopes={["chat:latest"]}
        failure={{
          id: "new-failure",
          role: "assistant",
          status: "failed",
          errorCode: "QUOTA_EXHAUSTED",
          updatedAt: Date.now() + 1000,
        }}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("当前额度已用尽");
    expect(screen.queryByText("附件读取失败")).not.toBeInTheDocument();
  });

  it("新错误直接覆盖旧错误，最新错误恢复后也不回退", () => {
    reportErrorNotice({
      messageSource: "public",
      id: "older",
      scope: "chat:group",
      message: "较早错误",
    });
    const { unmount } = render(<ChatErrorNotices scopes={["chat:group"]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("较早错误");
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "latest",
        scope: "chat:group",
        message: "最新错误",
      }),
    );
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.getByRole("alert")).toHaveTextContent("最新错误");
    expect(screen.queryByText("较早错误")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /另外/ }),
    ).not.toBeInTheDocument();
    act(() => dismissErrorNotices(["latest"]));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    unmount();
    render(<ChatErrorNotices scopes={["chat:group"]} />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("关闭最新错误不展示旧错误，且不影响其他对话", () => {
    reportErrorNotice({
      messageSource: "public",
      id: "first",
      scope: "chat:one",
      message: "第一条",
    });
    reportErrorNotice({
      messageSource: "public",
      id: "second",
      scope: "chat:one",
      message: "第二条",
    });
    reportErrorNotice({
      messageSource: "public",
      id: "other",
      scope: "chat:two",
      message: "其他对话",
    });
    const { rerender } = render(<ChatErrorNotices scopes={["chat:one"]} />);
    fireEvent.click(screen.getByRole("button", { name: "关闭错误提示" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    rerender(<ChatErrorNotices scopes={["chat:two"]} />);
    expect(screen.getByRole("alert")).toHaveTextContent("其他对话");
  });

  it("新的操作错误覆盖执行失败，操作恢复后不再展示旧执行错误", () => {
    const failure = {
      id: "failed-overwritten",
      role: "assistant" as const,
      status: "failed" as const,
      errorMessage: "旧执行失败",
      createdAt: Date.now() - 1000,
    };
    const { unmount } = render(
      <ChatErrorNotices scopes={["chat:overwrite"]} failure={failure} />,
    );
    act(() =>
      reportErrorNotice({
        messageSource: "public",
        id: "new-operation",
        scope: "chat:overwrite",
        message: "新操作失败",
      }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("新操作失败");
    act(() => dismissErrorNotices(["new-operation"]));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    unmount();
    render(<ChatErrorNotices scopes={["chat:overwrite"]} failure={failure} />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("人工等待超时错误提示", () => {
  it.each([
    [
      "INPUT_TIMEOUT",
      "waiting for user input timed out",
      "waiting for user input timed out",
    ],
    [
      "APPROVAL_TIMEOUT",
      "waiting for approval timed out",
      "waiting for approval timed out",
    ],
  ])("%s 用稳定失败码展示明确文案", (code, detail, label) => {
    render(
      <MessageFailure
        message={{
          id: "execution",
          role: "assistant",
          status: "failed",
          errorCode: code,
          errorMessage: detail,
          errorMessageSource: "public",
        }}
        last={true}
        placement="composer"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(label);
    expect(screen.getByText(detail)).toBeInTheDocument();
  });
});

it("消息失败按当前语言和参数重绘，并保留原生公开原因", async () => {
  const message = {
    id: "contract-message",
    executionId: "contract",
    role: "assistant" as const,
    status: "failed" as const,
    errorCode: "RuntimeWorkspaceScopeUnsupported",
    errorParams: { MaxDirectories: 2 },
    errorMessageSource: "localized" as const,
    content: "",
  };
  const { rerender, unmount } = render(
    <MessageFailure message={message} last placement="composer" />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent(
    "当前智能体最多支持 2 个项目目录，请调整工作范围",
  );
  await act(() => i18n.changeLanguage("en-US"));
  expect(screen.getByRole("alert")).toHaveTextContent(
    "the selected agent supports up to 2 project directories; adjust the workspace scope",
  );
  rerender(
    <MessageFailure
      message={{
        ...message,
        errorCode: "RuntimeQuotaExhausted",
        errorMessage: "Native quota exhausted",
        errorMessageSource: "public",
      }}
      last
      placement="composer"
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("Native quota exhausted");
  unmount();
  await i18n.changeLanguage("zh-CN");
});

it("稳定应用会话错误保留真实恢复动作", () => {
  const recover = vi.fn();
  render(
    <ChatErrorActions.Provider value={{ recover }}>
      <MessageFailure
        message={{
          id: "missing-session",
          role: "assistant",
          status: "failed",
          content: "",
          errorCode: "RuntimeSessionNotFound",
          errorMessageSource: "localized",
        }}
        last
        placement="composer"
      />
    </ChatErrorActions.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "在这里继续" }));
  expect(recover).toHaveBeenCalledTimes(1);
});
