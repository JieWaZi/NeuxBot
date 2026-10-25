import type { WheelEvent as ReactWheelEvent } from "react";

/** Dialog 的 RemoveScroll 只放行 content shard，Portal 浮层内需手动滚动。 */
export function consumeOverlayWheelScroll(event: ReactWheelEvent<HTMLElement>) {
  const element = event.currentTarget;
  const maxScroll = element.scrollHeight - element.clientHeight;
  if (maxScroll <= 0) return;

  const nextScrollTop = element.scrollTop + event.deltaY;
  const clamped = Math.max(0, Math.min(maxScroll, nextScrollTop));
  if (clamped === element.scrollTop) return;

  element.scrollTop = clamped;
  event.preventDefault();
  event.stopPropagation();
}
