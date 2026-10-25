import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;

type DialogContentProps = Omit<
  ComponentProps<typeof DialogPrimitive.Content>,
  "title"
> & {
  title: ReactNode;
  description?: string;
  children: ReactNode;
  closing?: boolean;
  visual?: ReactNode;
  headingLayout?: "default" | "centered";
  container?: HTMLElement;
  layout?: "content" | "form" | "stage";
  size?: "sm" | "md" | "lg" | "xl";
};

const dialogSizeClass = {
  sm: "max-w-[400px]",
  md: "max-w-[520px]",
  lg: "max-w-[720px]",
  xl: "max-w-[960px]",
} as const;

const dialogLayoutClass = {
  content: "",
  form: "h-[min(800px,calc(100dvh-48px))]",
} as const;

export function DialogContent({
  children,
  className,
  closing = false,
  visual,
  headingLayout = "default",
  container,
  description,
  layout = "content",
  size = "md",
  title,
  ...props
}: DialogContentProps) {
  return (
    <DialogPrimitive.Portal container={container}>
      <DialogPrimitive.Overlay
        data-closing={closing || undefined}
        data-motion="overlay"
        className={cn(
          "bg-overlay inset-0 z-50",
          container ? "absolute" : "fixed",
          layout !== "stage" && "backdrop-blur-sm",
        )}
      />
      <DialogPrimitive.Content
        data-motion={layout === "stage" ? "none" : "popup"}
        className={cn(
          "top-1/2 left-1/2 z-50 flex -translate-x-1/2 -translate-y-1/2 flex-col outline-none",
          container ? "absolute" : "fixed",
          layout === "stage"
            ? "w-auto max-w-[calc(100vw-40px)] overflow-visible bg-transparent"
            : cn(
                "border-border-default bg-surface-raised shadow-menu overflow-hidden rounded-2xl border",
                container
                  ? "max-h-[calc(100%-48px)] w-[calc(100%-48px)]"
                  : "max-h-[calc(100dvh-48px)] w-[calc(100vw-40px)]",
              ),
          layout === "stage" ? undefined : dialogSizeClass[size],
          layout === "stage" ? undefined : dialogLayoutClass[layout],
          className,
        )}
        {...props}
      >
        <NativeOverlayPresence />
        {layout === "stage" ? (
          <div className="relative">
            <DialogPrimitive.Title className="sr-only">
              {title}
            </DialogPrimitive.Title>
            <DialogPrimitive.Description className="sr-only">
              {description ?? title}
            </DialogPrimitive.Description>
            <DialogPrimitive.Close className="sr-only" aria-label="关闭">
              关闭
            </DialogPrimitive.Close>
            {children}
          </div>
        ) : (
          <>
            <header
              className={cn(
                "relative flex shrink-0 gap-4 px-6 pb-3",
                headingLayout === "centered"
                  ? "flex-col items-center pt-8 text-center"
                  : "items-start justify-between pt-5",
              )}
            >
              {visual}
              <DialogPrimitive.Title className="type-title min-w-0 pt-0.5 font-semibold">
                {title}
              </DialogPrimitive.Title>
              <DialogPrimitive.Close
                className={cn(
                  "text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring size-control-sm grid shrink-0 place-items-center rounded-sm outline-none focus-visible:ring-2",
                  headingLayout === "centered" && "absolute top-3 right-3",
                )}
                aria-label="关闭"
              >
                <X className="size-icon-md" strokeWidth={1.5} />
              </DialogPrimitive.Close>
            </header>
            {description ? (
              <DialogPrimitive.Description
                className={cn(
                  "text-muted-foreground type-body px-6 pb-4",
                  headingLayout === "centered" && "text-center",
                )}
              >
                {description}
              </DialogPrimitive.Description>
            ) : null}
            {children}
          </>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogFooter({
  className,
  ...props
}: ComponentProps<"footer">) {
  return (
    <footer
      className={cn(
        "border-border mt-auto flex min-h-16 shrink-0 items-center justify-end gap-2 border-t px-6 py-3",
        className,
      )}
      {...props}
    />
  );
}

export const DialogClose = DialogPrimitive.Close;
