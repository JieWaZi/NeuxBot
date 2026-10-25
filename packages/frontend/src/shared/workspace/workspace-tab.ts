type WorkspaceBaseTab = {
  id: string;
  label: string;
};

export type WorkspaceExecutionDiffSource = {
  chatId: string;
  messageId: string;
};

export type WorkspaceChatTab = WorkspaceBaseTab & {
  kind: "chat";
};

export type WorkspaceMemberTab = WorkspaceBaseTab & {
  chatId?: string;
  kind: "member";
  wakerId: string;
};

export type WorkspaceFileView = "code" | "preview";

export type WorkspaceTab = WorkspaceChatTab | WorkspaceMemberTab;
