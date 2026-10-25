import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const Sheet = DialogPrimitive.Root;

type SheetContentProps = ComponentProps<typeof DialogPrimitive.Content> & {
  title: string;
  size?: "default" | "wide";
  description?: string;
  children: ReactNode;
};

export function SheetContent({
  children,
  className,
  description,
  title,
  size = "default",
  ...props
}: SheetContentProps) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        data-motion="overlay"
        className="bg-overlay fixed inset-0 z-50 backdrop-blur-sm"
      />
      <DialogPrimitive.Content
        data-motion="view"
        className={cn(
          "border-border-default bg-surface-raised shadow-menu fixed inset-y-0 right-0 z-50 flex w-full flex-col border-l outline-none",
          size === "wide" ? "max-w-3xl" : "max-w-[400px]",
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        <header className="border-border flex min-h-16 items-center justify-between gap-6 border-b px-5">
          <div>
            <DialogPrimitive.Title className="type-title font-semibold">
              {title}
            </DialogPrimitive.Title>
            {description ? (
              <DialogPrimitive.Description className="text-muted-foreground type-body mt-1">
                {description}
              </DialogPrimitive.Description>
            ) : null}
          </div>
          <DialogPrimitive.Close
            className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring size-control-sm grid shrink-0 place-items-center rounded-sm outline-none focus-visible:ring-2"
            aria-label="关闭"
          >
            <X className="size-icon-md" strokeWidth={1.5} />
          </DialogPrimitive.Close>
        </header>
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
