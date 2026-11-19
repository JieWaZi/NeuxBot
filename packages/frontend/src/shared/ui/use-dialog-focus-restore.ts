import { useRef } from "react";

// 弹窗由业务状态打开，没有 Radix Trigger；关闭时把键盘焦点交还原操作。
export function useDialogFocusRestore(
  returnFocusTo?: HTMLElement | null | (() => HTMLElement | null),
) {
  const opener = useRef<HTMLElement | null>(null);
  return {
    onOpenAutoFocus: () => {
      opener.current =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
    },
    onCloseAutoFocus: (event: Event) => {
      const preferred =
        typeof returnFocusTo === "function" ? returnFocusTo() : returnFocusTo;
      const target = preferred?.isConnected ? preferred : opener.current;
      if (target?.isConnected && target !== document.body) {
        event.preventDefault();
        target.focus();
      }
    },
  };
}
