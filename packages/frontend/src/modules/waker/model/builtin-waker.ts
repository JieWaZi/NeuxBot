import type { Waker } from "./types";

/** 内置私人助理伙伴的固定标识，与后端 `PersonalAssistantWakerID` 一致。 */
export const PERSONAL_ASSISTANT_WAKER_ID = "7387cbf3f0d842c58d89e274f298216a";

/** 判断伙伴是否为系统内置、不可删除的默认伙伴。 */
export function isBuiltinWaker(
  waker: Pick<Waker, "id" | "isDefault">,
): boolean {
  return Boolean(waker.isDefault) || waker.id === PERSONAL_ASSISTANT_WAKER_ID;
}
