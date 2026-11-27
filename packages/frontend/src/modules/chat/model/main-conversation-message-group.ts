import type { GetMainConversationResponse } from "@/shared/api/generated/types.gen";
import type { PublicMessage } from "../api/main-conversation";

export type MainConversationWork = NonNullable<
  GetMainConversationResponse["activeWork"]
>[number];

export type MainConversationAvatarActivity = "working" | "input" | "approval";

export function mainConversationWorkRequest(work?: MainConversationWork) {
  if (work?.waitingElicitation) return "input";
  if (work?.waitingApproval) return "approval";
}

function turnKey(workChatId: string, wakerId: string, inputId: string) {
  return JSON.stringify([workChatId, wakerId, inputId]);
}

function assistantTurn(row: PublicMessage) {
  if (
    row.role === "assistant" &&
    row.id &&
    row.sourceChatId &&
    row.sourceWakerId &&
    row.replyToMessageId
  ) {
    return turnKey(row.sourceChatId, row.sourceWakerId, row.replyToMessageId);
  }
}

export function mainConversationMessageGroups(
  messages: PublicMessage[],
  activeWork: GetMainConversationResponse["activeWork"] = [],
) {
  const repliesByTurn = new Map<
    string,
    { first: PublicMessage; last: PublicMessage }
  >();
  const inputsById = new Map<string, PublicMessage>();
  const answeredInputs = new Set<string>();
  const turns = messages.map((row) => {
    const turn = assistantTurn(row);
    if (turn) {
      const replies = repliesByTurn.get(turn);
      if (replies) replies.last = row;
      else repliesByTurn.set(turn, { first: row, last: row });
    }
    if (row.role === "user" && row.id && row.sourceChatId) {
      inputsById.set(row.id, row);
    }
    if (row.role === "assistant" && row.replyToMessageId) {
      answeredInputs.add(row.replyToMessageId);
    }
    return turn;
  });
  const replyActivity = new Map<
    PublicMessage,
    MainConversationAvatarActivity
  >();
  const inlineWork = new Map<PublicMessage, MainConversationWork>();
  const inputsWithWork = new Set<PublicMessage>();
  const trailingWork: MainConversationWork[] = [];
  for (const work of activeWork) {
    const { workChatId, sourceWakerId, inputMessageId } = work;
    if (!workChatId) continue;
    const candidate = inputMessageId
      ? inputsById.get(inputMessageId)
      : undefined;
    const input =
      candidate?.sourceChatId === workChatId ? candidate : undefined;
    const turn =
      inputMessageId && sourceWakerId
        ? turnKey(workChatId, sourceWakerId, inputMessageId)
        : undefined;
    const replies = turn ? repliesByTurn.get(turn) : undefined;
    const request = mainConversationWorkRequest(work);
    // 本轮动效留在首条发言，待处理卡片跟随本轮最后一条发言。
    if (replies) replyActivity.set(replies.first, request ?? "working");
    const anchor = replies?.last ?? input;
    if (request && anchor) {
      inlineWork.set(anchor, work);
    } else if (request || (!replies && !work.hasPublicReply)) {
      trailingWork.push(work);
    } else {
      continue;
    }
    if (input) inputsWithWork.add(input);
  }

  return {
    rows: messages.map((row, index) => {
      const turn = turns[index];
      const continued = Boolean(turn && turn === turns[index - 1]);
      return {
        row,
        continued,
        showWorkRecord:
          row.role === "user" &&
          Boolean(
            row.id &&
            row.sequence &&
            row.sourceChatId &&
            !row.failureCode &&
            !answeredInputs.has(row.id) &&
            !inputsWithWork.has(row),
          ),
        work: inlineWork.get(row),
        replyActivity: replyActivity.get(row),
      };
    }),
    trailingWork,
  };
}
