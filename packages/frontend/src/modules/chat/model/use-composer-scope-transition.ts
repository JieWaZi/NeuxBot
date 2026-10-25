import { useCallback, useLayoutEffect, useRef, useState } from "react";

import { isDraftToChatScopeTransition } from "./composer-scope-transition";

/** 管理 composer 在对话/工作区切换时的清空策略，并保留 draft→chat 首条 create 后的输入。 */
export function useComposerScopeTransition(
  workspaceScopeKey: string,
  currentChatKey: string,
  onScopeChange: () => void,
) {
  const [composerClearRevision, setComposerClearRevision] = useState(0);
  const [companionScopeRevision, setCompanionScopeRevision] = useState(0);
  const skipComposerClearOnScopeChangeRef = useRef(false);
  const previousComposerScopeKey = useRef(currentChatKey);
  const previousWorkspaceScopeKey = useRef(workspaceScopeKey);

  const clearComposer = useCallback(() => {
    setComposerClearRevision((revision) => revision + 1);
  }, []);

  const markSkipComposerClearOnCreate = useCallback(() => {
    skipComposerClearOnScopeChangeRef.current = true;
  }, []);

  useLayoutEffect(() => {
    const previousScopeKey = previousWorkspaceScopeKey.current;
    previousWorkspaceScopeKey.current = workspaceScopeKey;
    if (previousComposerScopeKey.current === currentChatKey) return;
    previousComposerScopeKey.current = currentChatKey;
    const skipClear =
      skipComposerClearOnScopeChangeRef.current &&
      isDraftToChatScopeTransition(previousScopeKey, workspaceScopeKey);
    skipComposerClearOnScopeChangeRef.current = false;
    if (!skipClear) {
      clearComposer();
      setCompanionScopeRevision((revision) => revision + 1);
    }
    onScopeChange();
  }, [clearComposer, currentChatKey, onScopeChange, workspaceScopeKey]);

  return {
    composerClearRevision,
    companionScopeRevision,
    clearComposer,
    markSkipComposerClearOnCreate,
  };
}
