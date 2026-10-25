export const workspaceFileDragType = "application/x-ally-workspace-file";

export type WorkspaceFileDrag = {
  chatId?: string;
  rootPath: string;
  path: string;
};

export function readWorkspaceFileDrag(value: string): WorkspaceFileDrag | null {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object") return null;
    if (!("rootPath" in parsed) || typeof parsed.rootPath !== "string")
      return null;
    if (!("path" in parsed) || typeof parsed.path !== "string") return null;
    if (!parsed.rootPath || !parsed.path) return null;
    const chatId = "chatId" in parsed ? parsed.chatId : undefined;
    if (chatId !== undefined && typeof chatId !== "string") return null;
    return {
      rootPath: parsed.rootPath,
      path: parsed.path,
      chatId,
    };
  } catch {
    return null;
  }
}
