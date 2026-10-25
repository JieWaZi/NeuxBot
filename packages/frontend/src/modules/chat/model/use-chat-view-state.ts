import { useCallback, useState } from "react";

import type { ChatCodeReference } from "../runtime/chat-queue-store";
import type { PermissionLevel } from "@/shared/lib/runtime-permissions";

export type ChatViewState = {
  codeReferenceRequest?: ChatCodeReference;
  fileSource: "chat" | "project";
  model: string;
  openedCodeReference?: ChatCodeReference;
  permissionLevel?: PermissionLevel;
  query: string;
  reasoning: string;
  runtimeId: string;
  selectedCodeReferences: ChatCodeReference[];
  workspaceWarningCode: string;
};

function initialChatViewState(workspaceWarningCode: string): ChatViewState {
  return {
    fileSource: "project",
    model: "",
    query: "",
    reasoning: "",
    runtimeId: "",
    selectedCodeReferences: [],
    workspaceWarningCode,
  };
}

/**
 * 对话视图状态按对话或草稿范围隔离，切换范围时不依赖渲染后的清理 Effect。
 */
export function useChatViewState(
  scopeKey: string,
  initialWorkspaceWarningCode: string,
) {
  const [stateByScope, setStateByScope] = useState<
    Record<string, ChatViewState>
  >({});
  const state =
    stateByScope[scopeKey] ?? initialChatViewState(initialWorkspaceWarningCode);
  const update = useCallback(
    (patch: Partial<ChatViewState>) => {
      setStateByScope((current) => ({
        ...current,
        [scopeKey]: {
          ...(current[scopeKey] ??
            initialChatViewState(initialWorkspaceWarningCode)),
          ...patch,
        },
      }));
    },
    [initialWorkspaceWarningCode, scopeKey],
  );

  return { state, update };
}
