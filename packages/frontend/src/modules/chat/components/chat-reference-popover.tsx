import {
  ComposerPrimitive,
  unstable_useTriggerPopoverScopeContext,
  type Unstable_TriggerItem,
} from "@assistant-ui/react";
import {
  useEffect,
  useRef,
  type ComponentProps,
  type ReactNode,
  type RefObject,
} from "react";

export const chatReferencePopoverClass =
  "border-border bg-surface-elevated absolute right-0 bottom-full left-0 z-30 mb-2 overflow-hidden rounded-lg border p-1 shadow-sm";

export function ChatReferencePopoverScrollSync({
  scrollContainerRef,
}: {
  scrollContainerRef: RefObject<HTMLDivElement | null>;
}) {
  const { highlightedItemId, open } = unstable_useTriggerPopoverScopeContext();

  useEffect(() => {
    if (!open || !highlightedItemId) return;
    const item = document.getElementById(highlightedItemId);
    const container = scrollContainerRef.current;
    if (!item || !container) return;

    const itemTop = item.offsetTop;
    const itemBottom = itemTop + item.offsetHeight;
    const viewTop = container.scrollTop;
    const viewBottom = viewTop + container.clientHeight;
    if (itemTop < viewTop) {
      container.scrollTop = itemTop;
      return;
    }
    if (itemBottom > viewBottom) {
      container.scrollTop = itemBottom - container.clientHeight;
    }
  }, [highlightedItemId, open, scrollContainerRef]);

  return null;
}

export function ChatReferencePopoverItems({
  title,
  scrollContainerRef,
  children,
}: {
  title: string;
  scrollContainerRef: RefObject<HTMLDivElement | null>;
  children: (items: readonly Unstable_TriggerItem[]) => ReactNode;
}) {
  return (
    <ComposerPrimitive.Unstable_TriggerPopoverItems>
      {(items) => (
        <div ref={scrollContainerRef} className="max-h-80 overflow-y-auto">
          <ChatReferencePopoverScrollSync
            scrollContainerRef={scrollContainerRef}
          />
          <div className="text-muted-foreground type-caption px-2 py-1.5 font-medium">
            {title}
          </div>
          {children(items)}
        </div>
      )}
    </ComposerPrimitive.Unstable_TriggerPopoverItems>
  );
}

export function ChatReferencePopoverItem({
  item,
  index,
  icon,
  trailing,
  columns = "two",
}: {
  item: Unstable_TriggerItem;
  index: number;
  icon: ReactNode;
  trailing?: ReactNode;
  columns?: "two" | "three";
}) {
  const gridClass =
    columns === "three"
      ? "grid-cols-[20px_minmax(0,1fr)_20px]"
      : "grid-cols-[20px_minmax(0,1fr)]";

  return (
    <ComposerPrimitive.Unstable_TriggerPopoverItem
      key={item.id}
      item={item}
      index={index}
      className={`data-highlighted:bg-muted type-body min-h-row-rich grid w-full ${gridClass} items-center gap-2 rounded-md px-2 py-1 text-left outline-none`}
    >
      {icon}
      <span className="min-w-0">
        <span className="block truncate font-medium">{item.label}</span>
        {item.description ? (
          <span className="text-muted-foreground type-caption block truncate">
            {item.description}
          </span>
        ) : null}
      </span>
      {trailing}
    </ComposerPrimitive.Unstable_TriggerPopoverItem>
  );
}

export function ChatReferencePopoverMessage({
  tone = "muted",
  children,
}: {
  tone?: "muted" | "destructive";
  children: ReactNode;
}) {
  const className =
    tone === "destructive"
      ? "text-destructive type-body px-2 py-3 text-center"
      : "text-muted-foreground type-body px-2 py-3 text-center";
  return <p className={className}>{children}</p>;
}

export function ChatReferencePopoverList({
  children,
}: {
  children: ReactNode;
}) {
  return <div className="grid gap-0.5">{children}</div>;
}

type ChatReferencePopoverProps = {
  char: ComponentProps<
    typeof ComposerPrimitive.Unstable_TriggerPopover
  >["char"];
  adapter: NonNullable<
    ComponentProps<typeof ComposerPrimitive.Unstable_TriggerPopover>["adapter"]
  >;
  action: ComponentProps<
    typeof ComposerPrimitive.Unstable_TriggerPopover.Action
  >;
  title: string;
  ariaLabel: string;
  isLoading?: boolean;
  scrollContainerRef: RefObject<HTMLDivElement | null>;
  children: (items: readonly Unstable_TriggerItem[]) => ReactNode;
};

export function ChatReferencePopover({
  char,
  adapter,
  action,
  title,
  ariaLabel,
  isLoading = false,
  scrollContainerRef,
  children,
}: ChatReferencePopoverProps) {
  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      char={char}
      adapter={adapter}
      isLoading={isLoading}
      aria-label={ariaLabel}
      className={chatReferencePopoverClass}
    >
      <ComposerPrimitive.Unstable_TriggerPopover.Action {...action} />
      <ChatReferencePopoverItems
        title={title}
        scrollContainerRef={scrollContainerRef}
      >
        {children}
      </ChatReferencePopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  );
}

export function useReferencePopoverScrollRef() {
  return useRef<HTMLDivElement>(null);
}
