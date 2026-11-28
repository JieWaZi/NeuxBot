import type { ComponentProps, ReactNode } from "react";
import { useInRouterContext } from "react-router-dom";
import { useChatFork } from "./chat-fork";
import { ChatThread } from "./chat-thread";

type ThreadProps = ComponentProps<typeof ChatThread>;
type ForkSession = Omit<Parameters<typeof useChatFork>[0], "busy">;

type ChatConversationProps = Omit<ThreadProps, "fork" | "showComposer"> & {
  /** 会话入口提供原 Chat、可分支的历史边界及当前授权。 */
  forkSession?: ForkSession;
  /** 空值关闭输入区，省略时使用标准聊天输入区。 */
  composer?: ReactNode;
};

/** 普通聊天和来源工作记录共用消息、操作与输入区的装配。 */
export function ChatConversation({
  forkSession,
  composer,
  ...thread
}: ChatConversationProps) {
  const routed = useInRouterContext();
  const showComposer = composer === undefined;
  return (
    <>
      {routed && forkSession ? (
        <ForkableChatConversation
          {...thread}
          session={forkSession}
          showComposer={showComposer}
        />
      ) : (
        <ChatThread {...thread} showComposer={showComposer} />
      )}
      {composer}
    </>
  );
}

function ForkableChatConversation({
  session,
  ...thread
}: Omit<ThreadProps, "fork"> & { session: ForkSession }) {
  const fork = useChatFork({
    ...session,
    busy: Boolean(
      thread.isRunning ||
      thread.pendingApprovals?.length ||
      thread.pendingElicitations?.length,
    ),
  });
  return <ChatThread {...thread} fork={fork} />;
}
