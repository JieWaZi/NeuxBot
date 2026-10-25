import type { GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage } from "@/shared/api/generated/types.gen";
import type { WorkspaceFileEntry } from "@/shared/platform";
import type { WorkspaceFile } from "@/shared/workspace/types";

export function workspaceFilesOf(
  messages: readonly ChatMessage[],
): WorkspaceFile[] {
  const files = new Map<string, WorkspaceFile>();
  for (const message of messages) {
    for (const attachment of message.attachments ?? []) {
      const path = attachment.path?.trim();
      if (!path || files.has(path)) continue;
      files.set(path, {
        name: attachment.name?.trim() || path.split("/").at(-1) || path,
        path,
        source: "chat",
        mediaType: attachment.mediaType,
      });
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
      .map((file) => [file.path, file]),
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
