import type { ReactElement, ReactNode, RefObject } from "react";

import { Dialog, DialogContent, DialogTrigger } from "@/shared/ui/dialog";

export function ChatPreviewDialog({
  children,
  open,
  title,
  onOpenChange,
  trigger,
  returnFocusRef,
}: {
  children: ReactNode;
  open?: boolean;
  title: string;
  onOpenChange?: (open: boolean) => void;
  trigger?: ReactElement;
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger && <DialogTrigger asChild>{trigger}</DialogTrigger>}
      <DialogContent
        title={title}
        size="xl"
        className="w-[90vw] max-w-[90vw]"
        aria-describedby={undefined}
        onCloseAutoFocus={
          returnFocusRef &&
          ((event) => {
            const opener = returnFocusRef.current;
            if (opener?.isConnected) {
              event.preventDefault();
              opener.focus();
            }
          })
        }
      >
        <div className="min-h-0 flex-1 overflow-hidden px-6 pb-6">
          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}
