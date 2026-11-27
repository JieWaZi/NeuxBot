import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { subscribeChat } from "@/shared/api/events";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import {
  authorizedSourceChat,
  sourceHistoryOptions,
  type ConversationSubject,
} from "../api/main-conversation";
import { createChatEventBatcher } from "../runtime/chat-event-batcher";
import { applyChatLiveFrame, ChatLiveTracker } from "../runtime/chat-live";
import {
  mergeAuthoritativeMessages,
  type BotChatMessage,
} from "../runtime/chat-projection";

type DisplayPart = NonNullable<BotChatMessage["displayParts"]>[number];

/** 来源授权查询提供持久基线；进行中的增量沿用聊天流，断线后仍从同一主体读取。 */
export function useMainConversationSourceHistory(
  { subjectType, subjectId }: ConversationSubject,
  workChatId: string,
  active: boolean,
) {
  const client = useQueryClient();
  const options = useMemo(
    () => sourceHistoryOptions({ subjectType, subjectId }, workChatId),
    [subjectType, subjectId, workChatId],
  );
  const history = useInfiniteQuery({
    ...options,
    enabled: active,
    retry: false,
  });
  const chat = authorizedSourceChat(
    workChatId,
    history.data?.pages[0]?.chat,
    history.error,
  );
  const authorized = Boolean(chat);
  const [live, setLive] = useState<BotChatMessage[]>([]);
  useEffect(() => {
    if (!active || !authorized) return;
    const head = () => client.getQueryData(options.queryKey)?.pages[0];
    let tracker = new ChatLiveTracker();
    let closed = false;
    const overlay = new Map<string, BotChatMessage>();
    const batch = createChatEventBatcher(() => setLive([...overlay.values()]));
    const refresh = async (signal?: AbortSignal) => {
      await refreshQueryFromEvent(
        client,
        options.queryKey,
        () => client.fetchInfiniteQuery({ ...options, staleTime: 0 }),
        0,
        signal,
      );
      const page = head();
      if (!closed && page) {
        for (const row of page.resources ?? []) {
          const current = row.id && overlay.get(row.id);
          if (
            current &&
            row.id &&
            (row.revision ?? 0) >=
              (current.liveRevision ?? current.revision ?? 0)
          )
            overlay.delete(row.id);
        }
        setLive([...overlay.values()]);
      }
      return page?.latestSequence;
    };
    const stop = subscribeChat(workChatId, {
      after: head()?.latestSequence,
      isTerminal: () => false,
      reconcileCursor: async (signal) => {
        const sequence = await refresh(signal);
        tracker = new ChatLiveTracker();
        return sequence;
      },
      onEvent: (event) => {
        if (!event.transient) {
          const row = event.message;
          if (row?.id) {
            // 持久摘要可能先于正文检查点抵达；保留已经显示的更高版本增量。
            const local = overlay.get(row.id);
            const merged = local
              ? mergeAuthoritativeMessages([local], [row])[0]
              : row;
            if (merged) overlay.set(row.id, merged);
          }
          void refresh().catch(() => {});
          return;
        }
        const frame = event.live;
        if (!frame) return;
        const local = overlay.get(frame.messageId);
        const saved = head()?.resources?.find(
          (row) => row.id === frame.messageId,
        );
        const baseline = saved
          ? mergeAuthoritativeMessages(local ? [local] : [], [saved])[0]
          : local;
        const decision = tracker.accept(frame, baseline);
        if (decision === "recover")
          throw new Error("source chat live frame requires reconciliation");
        if (decision === "accept" && baseline) {
          overlay.set(frame.messageId, applyChatLiveFrame(baseline, frame));
          batch.push(event);
        }
      },
    });
    return () => {
      closed = true;
      stop();
      batch.close();
    };
  }, [active, authorized, client, options, workChatId]);

  const publications = useMemo(() => {
    const byExecution = new Map<string, Map<string, DisplayPart>>();
    for (const page of history.data?.pages.toReversed() ?? []) {
      for (const { id, executionId, content } of page.publishedMessages ?? []) {
        if (!id || !executionId || !content?.trim()) continue;
        let group = byExecution.get(executionId);
        if (!group) {
          group = new Map();
          byExecution.set(executionId, group);
        }
        if (!group.has(id)) group.set(id, { id, type: "text", text: content });
      }
    }
    return new Map(
      Array.from(byExecution, ([id, group]) => [id, [...group.values()]]),
    );
  }, [history.data]);
  const messages = useMemo(() => {
    if (!authorized || !history.data) return [];
    const native = mergeAuthoritativeMessages(
      live,
      history.data.pages.toReversed().flatMap((page) => page.resources ?? []),
    );
    return native.map((message) => {
      const publishedResultParts =
        message.role === "assistant" &&
        !message.question &&
        !message.inherited &&
        message.executionId
          ? publications.get(message.executionId)
          : undefined;
      return publishedResultParts
        ? { ...message, publishedResultParts }
        : message;
    });
  }, [authorized, history.data, live, publications]);
  return {
    history,
    messages,
    chat,
  };
}
