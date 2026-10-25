/** 判断内容是否实际进入前台窗口及各级滚动/裁剪区域，不以路由或挂载代替呈现。 */
export function isElementVisible(element: HTMLElement): boolean {
  if (
    !element.isConnected ||
    document.visibilityState !== "visible" ||
    !document.hasFocus() ||
    element.closest("[inert], [hidden], [aria-hidden='true']")
  )
    return false;
  const rectangle = element.getBoundingClientRect();
  if (rectangle.width <= 0 || rectangle.height <= 0) return false;
  let left = Math.max(0, rectangle.left);
  let right = Math.min(
    document.documentElement.clientWidth || window.innerWidth,
    rectangle.right,
  );
  let top = Math.max(0, rectangle.top);
  let bottom = Math.min(
    document.documentElement.clientHeight || window.innerHeight,
    rectangle.bottom,
  );
  for (
    let current: HTMLElement | null = element;
    current;
    current = current.parentElement
  ) {
    const style = window.getComputedStyle(current);
    if (
      style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse" ||
      style.opacity === "0"
    )
      return false;
    if (current !== element) {
      const clip = current.getBoundingClientRect();
      if (
        ["auto", "scroll", "hidden", "clip", "overlay"].includes(
          style.overflowX || style.overflow,
        )
      ) {
        left = Math.max(left, clip.left);
        right = Math.min(right, clip.right);
      }
      if (
        ["auto", "scroll", "hidden", "clip", "overlay"].includes(
          style.overflowY || style.overflow,
        )
      ) {
        top = Math.max(top, clip.top);
        bottom = Math.min(bottom, clip.bottom);
      }
    }
    if (right <= left || bottom <= top) return false;
  }
  return true;
}
