import type { ChatEvent } from "@/shared/api/events";

export type ChatEventBatcher = {
  push: (event: ChatEvent, flushImmediately?: boolean) => void;
  close: () => void;
};

export function createChatEventBatcher(
  onBatch: (events: ChatEvent[]) => void,
): ChatEventBatcher {
  let pending: ChatEvent[] = [];
  const pendingMessageIndices = new Map<string, number>();
  let timer: number | undefined;

  const flush = () => {
    if (timer !== undefined) window.clearTimeout(timer);
    timer = undefined;
    if (pending.length === 0) return;
    const batch = pending;
    pending = [];
    pendingMessageIndices.clear();
    onBatch(batch);
  };

  return {
    push(event, flushImmediately = false) {
      const messageID = event.message?.id;
      const pendingIndex = messageID
        ? pendingMessageIndices.get(messageID)
        : undefined;
      if (pendingIndex === undefined) {
        if (messageID) pendingMessageIndices.set(messageID, pending.length);
        pending.push(event);
      } else {
        // Runtime 事件携带的是完整消息快照；同一刷新窗口内只保留最新
        // 快照即可，避免重复归并和重复触发 Markdown 渲染。
        pending[pendingIndex] = event;
      }
      if (flushImmediately) {
        flush();
      } else if (timer === undefined) {
        timer = window.setTimeout(flush, 16);
      }
    },
    close() {
      if (timer !== undefined) window.clearTimeout(timer);
      timer = undefined;
      pending = [];
      pendingMessageIndices.clear();
    },
  };
}
