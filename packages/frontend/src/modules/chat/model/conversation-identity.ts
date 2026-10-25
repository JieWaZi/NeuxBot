import { useCallback, useLayoutEffect, useRef } from "react";

declare const conversationKeyBrand: unique symbol;

/** ChatScopeKey 把工作区 scope 与当前 Chat 原子绑定，禁止只按展示状态识别会话。 */
export type ChatScopeKey = string & {
  readonly [conversationKeyBrand]: true;
};

export type ChatInvocation = {
  chatId?: string;
  key: ChatScopeKey;
  queueKey: string;
};

export type ChatInvocationGuard = {
  capture: (chatId: string | undefined, queueKey: string) => ChatInvocation;
  isCurrent: (invocation: ChatInvocation) => boolean;
};

/** 使用稳定业务 ID 构建不可碰撞的会话身份。 */
export function conversationKeyOf(
  workspaceScopeKey: string,
  chatId?: string,
): ChatScopeKey {
  return JSON.stringify([workspaceScopeKey, chatId ?? null]) as ChatScopeKey;
}

/**
 * 为异步 handler 捕获调用时身份。layout effect 在用户交互前提交新身份，
 * 使旧请求的完成回调不能写入新会话。
 */
export function useChatInvocationGuard(
  currentKey: ChatScopeKey,
): ChatInvocationGuard {
  const currentKeyRef = useRef(currentKey);
  useLayoutEffect(() => {
    currentKeyRef.current = currentKey;
  }, [currentKey]);

  const capture = useCallback(
    (chatId: string | undefined, queueKey: string): ChatInvocation => ({
      chatId,
      key: currentKeyRef.current,
      queueKey,
    }),
    [],
  );
  const isCurrent = useCallback(
    (invocation: ChatInvocation) => currentKeyRef.current === invocation.key,
    [],
  );

  return { capture, isCurrent };
}
