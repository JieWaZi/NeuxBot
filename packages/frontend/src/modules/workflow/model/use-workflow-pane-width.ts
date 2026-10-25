import { useCallback, useState } from "react";

/** 画布内容挂载时开始测量；版本与运行窗格共享这段实际可用宽度。 */
export function useWorkflowPaneWidth() {
  const [available, setAvailable] = useState(0);
  const hostRef = useCallback((element: HTMLDivElement | null) => {
    if (!element) return;
    const width = element.getBoundingClientRect().width || element.offsetWidth;
    if (width > 0) setAvailable(width);
    const observer = new ResizeObserver(([entry]) => {
      if (entry && entry.contentRect.width > 0)
        setAvailable(entry.contentRect.width);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return { available, rightWidth: Math.min(320, available * 0.4), hostRef };
}
