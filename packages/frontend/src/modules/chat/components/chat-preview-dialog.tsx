import type { ReactElement, ReactNode } from "react";

import { Dialog, DialogContent, DialogTrigger } from "@/shared/ui/dialog";

export function ChatPreviewDialog({
  children,
  open,
  title,
  onOpenChange,
  trigger,
}: {
  children: ReactNode;
  open?: boolean;
  title: string;
  onOpenChange?: (open: boolean) => void;
  trigger?: ReactElement;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger && <DialogTrigger asChild>{trigger}</DialogTrigger>}
      <DialogContent
        title={title}
        size="xl"
        className="w-[90vw] max-w-[90vw]"
        aria-describedby={undefined}
      >
        <div className="min-h-0 flex-1 overflow-hidden px-6 pb-6">
          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}
