import type { BotChatMessage } from "../runtime/chat-projection";
import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDelegation as ChannelDelegation } from "@/shared/api/generated/types.gen";

type TeamMainChatMessage = {
  actor?: string;
  role?: string;
};

type TeamDelegationGroup = {
  id: string;
  direction: "incoming" | "outgoing";
  beforeMessageId?: string;
  createdAt: number;
  latestAt: number;
  partnerIds: string[];
};

/** 同一消息前的委派合为一行；未定位的记录独立保留，避免跨回合去重。 */
export function groupTeamDelegations(
  delegations: readonly ChannelDelegation[],
  viewingMemberId: string,
  messages: Parameters<typeof teamDelegationPosition>[2],
): TeamDelegationGroup[] {
  const groups = new Map<string, TeamDelegationGroup>();
  for (const task of delegations) {
    if (
      !task.id ||
      !task.senderWakerId ||
      !task.assigneeWakerId ||
      task.createdAt === undefined ||
      !Number.isFinite(task.createdAt) ||
      task.senderWakerId === task.assigneeWakerId
    )
      continue;
    const direction =
      task.senderWakerId === viewingMemberId
        ? "outgoing"
        : task.assigneeWakerId === viewingMemberId
          ? "incoming"
          : undefined;
    if (!direction) continue;
    const targetId =
      direction === "outgoing" ? task.assigneeWakerId : task.senderWakerId;
    const position = teamDelegationPosition(
      direction,
      { ...task, createdAt: task.createdAt },
      messages,
    );
    const id = JSON.stringify([
      direction,
      position.beforeMessageId ? "message" : "task",
      position.beforeMessageId ?? task.id,
    ]);
    const group = groups.get(id);
    if (group) {
      if (!group.partnerIds.includes(targetId)) group.partnerIds.push(targetId);
      group.createdAt = Math.min(group.createdAt, position.createdAt);
      group.latestAt = Math.max(group.latestAt, task.createdAt);
    } else {
      groups.set(id, {
        id: `delegation:${id}`,
        direction,
        ...position,
        latestAt: task.createdAt,
        partnerIds: [targetId],
      });
    }
  }
  return [...groups.values()];
}

/** 收件绑定执行输入，发件绑定发起任务的回复；接纳回执时间仅用于标签展示。 */
export function teamDelegationPosition(
  direction: "incoming" | "outgoing",
  delegation: {
    executionId?: string;
    queuedAt?: number;
    sentAt?: number;
    createdAt: number;
  },
  messages: readonly Pick<
    BotChatMessage,
    "id" | "role" | "executionId" | "createdAt" | "updatedAt"
  >[],
): { beforeMessageId?: string; createdAt: number } {
  const createdAt =
    (direction === "incoming" ? delegation.queuedAt : delegation.sentAt) ||
    delegation.createdAt;
  if (direction === "incoming" && delegation.executionId) {
    const replyIndex = messages.findIndex(
      (message) =>
        message.role === "assistant" &&
        message.executionId === delegation.executionId,
    );
    if (replyIndex >= 0) {
      let anchor = messages[replyIndex];
      for (let index = replyIndex - 1; index >= 0; index--) {
        const previous = messages[index];
        if (previous?.role === "assistant") break;
        if (previous?.role === "user") {
          anchor = previous;
          break;
        }
      }
      return { beforeMessageId: anchor?.id, createdAt };
    }
  }
  if (direction === "outgoing") {
    const precedingReplies = messages.filter(
      (message) =>
        message.role === "assistant" &&
        (message.createdAt ?? Infinity) <= createdAt,
    );
    // 延期派发发生在负责人的回复结束后；旧记录没有来源执行身份时沿最近回合定位。
    const reply =
      precedingReplies.findLast(
        (message) => (message.updatedAt ?? Infinity) >= createdAt,
      ) ?? precedingReplies.at(-1);
    if (reply?.id) return { beforeMessageId: reply.id, createdAt };
  }
  return { createdAt };
}

/**
 * 团队主会话展示真实用户输入和持久成员报告；system/team_lead 输入是隐藏的协调协议。
 */
export function teamMainChatMessageVisible(message: TeamMainChatMessage) {
  return (
    message.role !== "user" ||
    message.actor === "user" ||
    message.actor === "team_member"
  );
}

/**
 * 团队主会话就是负责人 Chat，主会话和成员 Tab 都展示各自 Chat 的 token/context 用量。
 */
export function teamChatPresentation() {
  return {
    showUsageMetrics: true,
  };
}
