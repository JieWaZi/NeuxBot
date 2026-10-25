type HorizontalDragOptions = {
  initialValue: number;
  valueAt: (clientX: number) => number;
  initialX: number;
  onChange: (value: number) => void;
  onCommit: (value: number) => void;
  onFinish: () => void;
};

/** 每帧使用最新位置；结束时保存最终值，卸载时只清理和保存。 */
export function trackHorizontalDrag(options: HorizontalDragOptions) {
  let latestX = options.initialX;
  let frame = 0;
  let finished = false;
  const previousSelect = document.body.style.userSelect;
  const previousCursor = document.body.style.cursor;
  document.body.style.userSelect = "none";
  document.body.style.cursor = "col-resize";

  const onMove = (event: MouseEvent) => {
    latestX = event.clientX;
    if (frame) return;
    frame = window.requestAnimationFrame(() => {
      frame = 0;
      options.onChange(options.valueAt(latestX));
    });
  };
  const finish = (updateView: boolean) => {
    if (finished) return;
    finished = true;
    window.cancelAnimationFrame(frame);
    document.removeEventListener("mousemove", onMove);
    document.removeEventListener("mouseup", onUp);
    window.removeEventListener("blur", onBlur);
    document.body.style.userSelect = previousSelect;
    document.body.style.cursor = previousCursor;
    const value = options.valueAt(latestX);
    if (updateView) {
      options.onChange(value);
      options.onFinish();
    }
    if (!Object.is(value, options.initialValue)) options.onCommit(value);
  };
  const onUp = (event: MouseEvent) => {
    latestX = event.clientX;
    finish(true);
  };
  const onBlur = () => finish(true);
  document.addEventListener("mousemove", onMove);
  document.addEventListener("mouseup", onUp);
  window.addEventListener("blur", onBlur);
  return () => finish(false);
}
