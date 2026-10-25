import { act, renderHook } from "@testing-library/react";
import { expect, it } from "vitest";
import { useChatAccountSelection } from "./use-chat-account-selection";

it("账号切换在发送失败或页面重挂载后仍按聊天保留", () => {
  const key = "ally.account.chat:account-persistence";
  window.localStorage.removeItem(key);
  const first = renderHook(() =>
    useChatAccountSelection("chat:account-persistence"),
  );
  act(() => first.result.current.selectAccount("account-a"));
  first.unmount();
  const second = renderHook(() =>
    useChatAccountSelection("chat:account-persistence"),
  );
  expect(second.result.current.accountId).toBe("account-a");
  const other = renderHook(() => useChatAccountSelection("chat:other-account"));
  expect(other.result.current.accountId).toBeUndefined();
  window.localStorage.removeItem(key);
});
