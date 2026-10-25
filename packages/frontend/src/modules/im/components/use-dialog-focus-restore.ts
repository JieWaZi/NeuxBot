import { useRef } from "react";

// IM 弹窗由业务状态打开，没有 Radix Trigger；关闭时把键盘焦点交还原操作。
export function useDialogFocusRestore(returnFocusTo?: HTMLElement | null) {
  const opener = useRef<HTMLElement | null>(null);
  return {
    onOpenAutoFocus: () => {
      opener.current =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
    },
    onCloseAutoFocus: (event: Event) => {
      const target = returnFocusTo?.isConnected
        ? returnFocusTo
        : opener.current;
      if (target?.isConnected && target !== document.body) {
        event.preventDefault();
        target.focus();
      }
    },
  };
}
