import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  conversationKeyOf,
  useChatInvocationGuard,
} from "./conversation-identity";
import { useChatInteractions } from "./use-conversation-interactions";
import { useErrorNotices } from "@/shared/api/error-notices";

describe("useChatInteractions", () => {
  it("审批和补充输入恢复成功分别清除各自错误，不因相同文案合并", async () => {
    const resolveApproval = vi
      .fn()
      .mockRejectedValueOnce(new Error("失败"))
      .mockResolvedValueOnce(undefined);
    const resolveElicitation = vi
      .fn()
      .mockRejectedValueOnce(new Error("失败"))
      .mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => {
      const key = conversationKeyOf("scope:one", "chat-one");
      const interactions = useChatInteractions({
        chatId: "chat-one",
        conversationKey: key,
        guard: useChatInvocationGuard(key),
        queueKey: "chat-one",
        pendingApprovals: [{ approvalId: "approval-one" }],
        pendingElicitations: [{ elicitationId: "input-one" }],
        resolveApproval,
        resolveElicitation,
        onSettled: vi.fn(),
      });
      return { ...interactions, notices: useErrorNotices() };
    });
    await act(async () => {
      await result.current.resolveApprovalChoice("approval-one", "allow");
      await result.current.resolveElicitationChoice("input-one", {}, false);
    });
    expect(result.current.notices).toHaveLength(2);
    await act(async () => {
      await result.current.resolveApprovalChoice("approval-one", "allow");
    });
    expect(result.current.notices.map((notice) => notice.operation)).toEqual([
      "elicitation:chat-one:input-one",
    ]);
    await act(async () => {
      await result.current.resolveElicitationChoice("input-one", {}, false);
    });
    expect(result.current.notices).toHaveLength(0);
  });
  it("切换 ChatScopeKey 后晚到审批结果只刷新原 Chat", async () => {
    let finishApproval: (() => void) | undefined;
    const resolveApproval = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishApproval = resolve;
        }),
    );
    const onSettled = vi.fn();
    const { result, rerender } = renderHook(
      ({ chatId, scopeKey }: { chatId: string; scopeKey: string }) => {
        const chatScopeKey = conversationKeyOf(scopeKey, chatId);
        return useChatInteractions({
          chatId,
          conversationKey: chatScopeKey,
          guard: useChatInvocationGuard(chatScopeKey),
          onSettled,
          pendingApprovals: [{ approvalId: "approval-1" }],
          pendingElicitations: [],
          queueKey: chatId,
          resolveApproval,
          resolveElicitation: vi.fn(),
        });
      },
      {
        initialProps: { chatId: "chat-1", scopeKey: "team:one:thread:one" },
      },
    );

    act(() => {
      void result.current.resolveApprovalChoice("approval-1", "allow-once");
    });
    await waitFor(() =>
      expect(result.current.approvalSubmissionState).toBe("submitting"),
    );

    rerender({ chatId: "chat-2", scopeKey: "team:two:thread:two" });
    expect(result.current.approvalSubmissionState).toBe("idle");
    await act(async () => {
      finishApproval?.();
      await Promise.resolve();
    });

    expect(result.current.approvalSubmissionState).toBe("idle");
    expect(onSettled).toHaveBeenCalledWith("chat-1");
  });
});
