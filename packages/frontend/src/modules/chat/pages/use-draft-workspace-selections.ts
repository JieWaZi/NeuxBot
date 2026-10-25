import { useState } from "react";

type WorkspaceSelection =
  { type: "folder"; path: string } | { type: "project"; projectId: string };
type WorkspaceSelections = Record<string, WorkspaceSelection>;

export const draftWorkspaceStorageKey = "ally.chat.workspace-selections";

function readSelections(): WorkspaceSelections {
  try {
    const raw = window.localStorage.getItem(draftWorkspaceStorageKey);
    const value: unknown = raw ? JSON.parse(raw) : null;
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    const selections: WorkspaceSelections = {};
    for (const [scopeId, selection] of Object.entries(value)) {
      if (!scopeId || !selection || typeof selection !== "object") continue;
      if (
        "type" in selection &&
        selection.type === "folder" &&
        "path" in selection &&
        typeof selection.path === "string" &&
        selection.path.trim()
      ) {
        selections[scopeId] = { type: "folder", path: selection.path };
      } else if (
        "type" in selection &&
        selection.type === "project" &&
        "projectId" in selection &&
        typeof selection.projectId === "string" &&
        selection.projectId.trim()
      ) {
        selections[scopeId] = {
          type: "project",
          projectId: selection.projectId,
        };
      }
    }
    return selections;
  } catch {
    return {};
  }
}

export function useDraftWorkspaceSelections() {
  const [selections, setSelections] = useState(readSelections);
  const selectWorkspace = (scopeId: string, selection: WorkspaceSelection) => {
    const next = { ...selections, ...readSelections(), [scopeId]: selection };
    setSelections(next);
    try {
      window.localStorage.setItem(
        draftWorkspaceStorageKey,
        JSON.stringify(next),
      );
    } catch {
      // 本地偏好不可用时，当前会话仍可继续选择和发送。
    }
  };
  return [selections, selectWorkspace] as const;
}
