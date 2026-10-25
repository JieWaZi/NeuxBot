import type { MouseEvent } from "react";
import type { NavigateFunction, To } from "react-router-dom";

export function navigateRouteImmediately(
  event: MouseEvent<HTMLElement>,
  navigate: NavigateFunction,
  to: To,
) {
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  ) {
    return;
  }
  event.preventDefault();
  void navigate(to, { flushSync: true });
}
