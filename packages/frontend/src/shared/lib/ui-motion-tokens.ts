/** 将当前主题的布局动效 Token 转成帧动画和 Motion 可共同使用的参数，时长单位为秒。 */
export function readLayoutMotionTokens(durationToken = "--duration-layout") {
  const tokens = getComputedStyle(document.documentElement);
  const duration = Number.parseFloat(tokens.getPropertyValue(durationToken));
  const [x1, y1, x2, y2] =
    tokens
      .getPropertyValue("--ease-ui")
      .match(/-?\d*\.?\d+/g)
      ?.map(Number) ?? [];
  const ease: [number, number, number, number] | undefined =
    x1 !== undefined && y1 !== undefined && x2 !== undefined && y2 !== undefined
      ? [x1, y1, x2, y2]
      : undefined;
  return { duration: Number.isFinite(duration) ? duration / 1000 : 0, ease };
}
