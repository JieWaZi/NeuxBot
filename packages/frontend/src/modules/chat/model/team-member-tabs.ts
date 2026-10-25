import type { WorkspaceTab } from "@/shared/workspace/workspace-tab";

type TeamMemberChatIdentity = {
  chatId?: string;
  id: string;
};

/** 返回已不属于当前 Thread 快照的成员 Tab；调用方负责按 workspace 生命周期关闭。 */
export function obsoleteTeamMemberTabIds(
  tabs: readonly WorkspaceTab[],
  members: readonly TeamMemberChatIdentity[],
): string[] {
  const chatIdByWaker = new Map(
    members.flatMap((member) =>
      member.chatId ? [[member.id, member.chatId] as const] : [],
    ),
  );
  return tabs.flatMap((tab) =>
    tab.kind === "member" && chatIdByWaker.get(tab.wakerId) !== tab.chatId
      ? [tab.id]
      : [],
  );
}
