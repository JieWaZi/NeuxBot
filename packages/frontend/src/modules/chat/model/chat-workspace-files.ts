import type { GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage } from "@/shared/api/generated/types.gen";
import type { WorkspaceFileEntry } from "@/shared/platform";
import type { WorkspaceFile } from "@/shared/workspace/types";

export function workspaceFileKey(
  file: Pick<WorkspaceFile, "path" | "sourceChatId">,
) {
  return file.sourceChatId
    ? JSON.stringify([file.sourceChatId, file.path])
    : file.path;
}

export function workspaceFilesOf(
  messages: readonly (Pick<ChatMessage, "attachments"> & {
    sourceChatId?: string;
  })[],
): WorkspaceFile[] {
  const files = new Map<string, WorkspaceFile>();
  for (const message of messages) {
    for (const attachment of message.attachments ?? []) {
      const path = attachment.path?.trim();
      if (!path) continue;
      const file: WorkspaceFile = {
        name: attachment.name?.trim() || path.split("/").at(-1) || path,
        path,
        source: "chat",
        mediaType: attachment.mediaType,
        ...(message.sourceChatId ? { sourceChatId: message.sourceChatId } : {}),
      };
      const key = workspaceFileKey(file);
      if (!files.has(key)) files.set(key, file);
    }
  }
  return [...files.values()];
}

export function workspaceFilesWithGenerated(
  files: readonly WorkspaceFile[],
  generatedEntries: readonly WorkspaceFileEntry[],
  removedPaths: readonly string[] = [],
): WorkspaceFile[] {
  const removed = new Set(removedPaths);
  const merged = new Map(
    files
      .filter((file) => !removed.has(file.path))
      .map((file) => [workspaceFileKey(file), file]),
  );
  for (const entry of generatedEntries) {
    const path = entry.relativePath.trim();
    if (!path || removed.has(path) || merged.has(path)) continue;
    merged.set(path, {
      name: entry.name.trim() || path.split("/").at(-1) || path,
      path,
      source: "chat",
    });
  }
  return [...merged.values()];
}
