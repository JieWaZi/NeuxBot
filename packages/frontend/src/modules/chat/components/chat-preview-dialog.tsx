import type { ReactNode } from "react";

import { Dialog, DialogContent } from "@/shared/ui/dialog";

export function ChatPreviewDialog({
  children,
  open,
  title,
  onOpenChange,
}: {
  children: ReactNode;
  open: boolean;
  title: string;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={title} size="xl" className="w-[90vw] max-w-[90vw]">
        <div className="min-h-0 flex-1 overflow-hidden px-6 pb-6">
          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}
