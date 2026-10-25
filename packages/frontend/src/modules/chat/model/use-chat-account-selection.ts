import { useCallback, useState } from "react";

function preferenceKey(scopeKey: string) {
  return scopeKey.startsWith("chat:") ? `ally.account.${scopeKey}` : undefined;
}

function savedAccount(scopeKey: string) {
  const key = preferenceKey(scopeKey);
  if (!key) return undefined;
  try {
    return window.localStorage.getItem(key) || undefined;
  } catch {
    return undefined;
  }
}

/** 账号选择属于实际聊天，成员 Tab 不共用父工作区的偏好。 */
export function useChatAccountSelection(scopeKey: string) {
  const [selections, setSelections] = useState<Record<string, string>>({});
  const accountId = selections[scopeKey] ?? savedAccount(scopeKey);
  const selectAccount = useCallback(
    (id: string) => {
      const key = preferenceKey(scopeKey);
      if (key) {
        try {
          window.localStorage.setItem(key, id);
        } catch {
          // 本地存储不可用时，当前页面仍保留所选账号。
        }
      }
      setSelections((current) => ({ ...current, [scopeKey]: id }));
    },
    [scopeKey],
  );
  return { accountId, selectAccount };
}
