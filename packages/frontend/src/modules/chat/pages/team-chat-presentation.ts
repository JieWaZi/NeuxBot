type TeamMainChatMessage = {
  actor?: string;
  role?: string;
};

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
