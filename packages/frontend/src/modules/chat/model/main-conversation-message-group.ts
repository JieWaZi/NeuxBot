import type { GetMainConversationResponse } from "@/shared/api/generated/types.gen";
import type { PublicMessage } from "../api/main-conversation";

export type MainConversationWork = NonNullable<
  GetMainConversationResponse["activeWork"]
>[number];

export type MainConversationRequest = "input" | "approval";

const messageTimeGapMs = 15 * 60 * 1000;

export function mainConversationWorkRequest(work?: MainConversationWork) {
  if (work?.waitingElicitation) return "input";
  if (work?.waitingApproval) return "approval";
}

function turnKeys(
  chatId: string | undefined,
  wakerId: string | undefined,
  executionId: string | undefined,
  inputId: string | undefined,
) {
  if (!chatId || !wakerId) return;
  return {
    execution: executionId && JSON.stringify([chatId, wakerId, executionId]),
    input: inputId && JSON.stringify([chatId, wakerId, inputId]),
  };
}

export function mainConversationMessageGroups(
  messages: PublicMessage[],
  activeWork: GetMainConversationResponse["activeWork"] = [],
  pendingMainInputs?: ReadonlySet<string>,
) {
  const lastReplyByExecution = new Map<string, PublicMessage>();
  const lastReplyByInput = new Map<string, PublicMessage>();
  const lastLegacyReplyByInput = new Map<string, PublicMessage>();
  const inputsById = new Map<string, PublicMessage>();
  const answeredInputs = new Set<string>();
  const turns = messages.map((row) => {
    const turn =
      row.role === "assistant" && row.id
        ? turnKeys(
            row.sourceChatId,
            row.sourceWakerId,
            row.forkExecutionId,
            row.replyToMessageId,
          )
        : undefined;
    if (turn?.execution) lastReplyByExecution.set(turn.execution, row);
    if (turn?.input) {
      lastReplyByInput.set(turn.input, row);
      if (!turn.execution) lastLegacyReplyByInput.set(turn.input, row);
    }
    if (row.role === "user" && row.id && row.sourceChatId) {
      inputsById.set(row.id, row);
    }
    if (row.role === "assistant" && row.replyToMessageId) {
      answeredInputs.add(row.replyToMessageId);
    }
    return turn;
  });
  const inlineWork = new Map<PublicMessage, MainConversationWork>();
  const waitingWork = new Map<PublicMessage, MainConversationWork>();
  const inputsWithWork = new Set<PublicMessage>();
  const trailingWork: MainConversationWork[] = [];
  for (const work of activeWork) {
    const { workChatId, sourceWakerId, executionId, inputMessageId } = work;
    if (!workChatId) continue;
    const candidate = inputMessageId
      ? inputsById.get(inputMessageId)
      : undefined;
    const input =
      candidate?.sourceChatId === workChatId ? candidate : undefined;
    if (!mainConversationWorkRequest(work)) {
      if (input && executionId && !work.hasPublicReply)
        waitingWork.set(input, work);
      continue;
    }
    const turn = turnKeys(
      workChatId,
      sourceWakerId,
      executionId,
      inputMessageId,
    );
    const exactReply = turn?.execution
      ? lastReplyByExecution.get(turn.execution)
      : undefined;
    const fallbackReply = turn?.input
      ? (turn.execution ? lastLegacyReplyByInput : lastReplyByInput).get(
          turn.input,
        )
      : undefined;
    // 续跑报告可能指向原工单交办。只有缺少执行身份的记录按交办匹配。
    const anchor = exactReply ?? fallbackReply ?? input;
    if (anchor) inlineWork.set(anchor, work);
    else trailingWork.push(work);
    if (input) inputsWithWork.add(input);
  }

  let previousTime: Date | undefined;
  return {
    rows: messages.map((row, index) => {
      const time = new Date(row.createdAt ?? 0);
      const timestamp = time.getTime();
      const validTime = Boolean(row.id) && timestamp > 0;
      const startsTimeGroup =
        validTime &&
        (!previousTime ||
          timestamp - previousTime.getTime() > messageTimeGapMs ||
          (timestamp > previousTime.getTime() &&
            time.toDateString() !== previousTime.toDateString()));
      if (validTime) previousTime = time;
      const turn = turns[index];
      const previous = turns[index - 1];
      const continued =
        turn?.execution && previous?.execution
          ? turn.execution === previous.execution
          : Boolean(turn?.input && turn.input === previous?.input);
      const waiting = waitingWork.get(row);
      return {
        row,
        timeDivider: startsTimeGroup ? time : undefined,
        continued,
        showWorkRecord:
          row.role === "user" &&
          Boolean(
            row.id &&
            row.sequence &&
            row.sourceChatId &&
            !answeredInputs.has(row.id) &&
            !inputsWithWork.has(row),
          ),
        work: inlineWork.get(row),
        waitingWork: waiting,
        awaitingReply:
          row.role === "user" &&
          Boolean(
            row.id &&
            !row.failureCode &&
            !answeredInputs.has(row.id) &&
            !inputsWithWork.has(row) &&
            (waiting || pendingMainInputs?.has(row.id)),
          ),
      };
    }),
    trailingWork,
  };
}
