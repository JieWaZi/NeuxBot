import { ArrowUp, ChevronDown, ChevronUp, LoaderCircle } from "lucide-react";
import {
  forwardRef,
  useId,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

type ComposerSurfaceProps = ComponentProps<"div"> & {
  bodyClassName?: string;
  context?: ReactNode;
  contextLayers?: ReactNode;
  leadingActions?: ReactNode;
  trailingActions?: ReactNode;
  visualEffect?: ReactNode;
  expanded?: boolean;
  invalid?: boolean;
};

const composerSurfaceClassName =
  "border-field-border bg-surface-raised focus-within:border-field-border-active focus-within:ring-ring shadow-composer data-[invalid]:border-destructive relative rounded-2xl border transition-[border-color,box-shadow] focus-within:ring-2";

/** Composer 的叠层容器，按 DOM 顺序让后面的表面覆盖前面的底边。 */
export function ComposerStack({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-chat-composer-stack
      className={cn("relative isolate", className)}
      {...props}
    />
  );
}

/** 工作范围、待发送消息和团队状态共用同一套 Composer 顶栏布局。 */
export function ComposerContext({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      data-chat-composer-context-bar
      className={cn(
        "type-caption flex min-w-0 items-center gap-2 px-3 pt-2 pb-1.5",
        className,
      )}
      {...props}
    />
  );
}

/** 普通聊天与独立指令输入共用正文留白及最小编辑高度。 */
export function ComposerInputBody({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div className={cn("min-h-16 px-3 pt-3 pb-1", className)} {...props} />
  );
}

/** 多条 Composer 上下文自动收拢；两种状态都在自身范围内滚动。 */
export function ComposerLayerStack({
  children,
  count,
  label,
  expandLabel,
  collapseLabel,
  footer,
}: {
  children: ReactNode;
  count: number;
  label: string;
  expandLabel: string;
  collapseLabel: string;
  footer?: ReactNode;
}) {
  const viewportId = useId();
  const [expanded, setExpanded] = useState(false);
  const overflowing = count > 3;
  if (!overflowing && expanded) setExpanded(false);
  const collapsed = overflowing && !expanded;

  return (
    <div
      data-chat-composer-layer-stack
      data-collapsed={collapsed || undefined}
      className="relative"
    >
      {overflowing ? (
        <ComposerContext>
          <span className="text-muted-foreground min-w-0 flex-1">{label}</span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={expanded ? collapseLabel : expandLabel}
            title={expanded ? collapseLabel : expandLabel}
            aria-expanded={expanded}
            aria-controls={viewportId}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? <ChevronUp /> : <ChevronDown />}
          </Button>
        </ComposerContext>
      ) : null}
      {count > 0 ? (
        <div
          id={viewportId}
          role="region"
          aria-label={label}
          tabIndex={0}
          data-chat-queue-list
          className={cn(
            "focus-visible:ring-ring [&>div+div]:before:bg-border-subtle relative overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-inset [&>div+div]:relative [&>div+div]:before:absolute [&>div+div]:before:inset-x-3 [&>div+div]:before:top-0 [&>div+div]:before:h-px",
            collapsed ? "max-h-20" : "max-h-60",
          )}
        >
          {children}
        </div>
      ) : null}
      {count > 0 && footer ? (
        <div aria-hidden="true" className="bg-border-subtle mx-3 h-px" />
      ) : null}
      {footer}
    </div>
  );
}

export function ComposerSurface({
  bodyClassName,
  children,
  className,
  context,
  contextLayers,
  expanded = false,
  invalid = false,
  leadingActions,
  trailingActions,
  visualEffect,
  ...props
}: ComposerSurfaceProps) {
  const surface = (
    <div
      data-expanded={expanded || undefined}
      data-invalid={invalid || undefined}
      data-chat-composer-surface
      className={cn(composerSurfaceClassName, className)}
      {...props}
    >
      {visualEffect}
      <div
        className={cn(
          "px-3",
          visualEffect && "relative z-10",
          expanded ? "py-3" : "py-2",
          bodyClassName,
        )}
      >
        {children}
      </div>
      {leadingActions || trailingActions ? (
        <div
          className={cn(
            "min-h-control-md flex items-center justify-between gap-3 px-2 pb-2",
            visualEffect && "relative z-10",
          )}
        >
          <div className="flex min-w-0 items-center gap-1">
            {leadingActions}
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {trailingActions}
          </div>
        </div>
      ) : null}
    </div>
  );

  if (!context && !contextLayers) return surface;

  return (
    <ComposerStack
      data-has-context="true"
      data-chat-composer-frame
      className="bg-composer-context rounded-2xl"
    >
      {contextLayers}
      {contextLayers && context ? (
        <div aria-hidden="true" className="bg-border-subtle mx-3 h-px" />
      ) : null}
      {context ? <ComposerContext>{context}</ComposerContext> : null}
      {surface}
    </ComposerStack>
  );
}

type ComposerSendButtonProps = ComponentProps<typeof Button>;

/** Composer 发送按钮：28px 圆形 primary + 上箭头。 */
export function ComposerSendButton({
  className,
  ...props
}: ComposerSendButtonProps) {
  return (
    <Button
      type="button"
      shape="circle"
      size="icon-sm"
      variant="primary"
      className={cn("shrink-0", className)}
      {...props}
    >
      <ArrowUp />
    </Button>
  );
}

type ComposerStopButtonProps = ComponentProps<"button"> & {
  loading?: boolean;
};

/** Composer 停止按钮：深色圆底 + 方形停止指示。 */
export const ComposerStopButton = forwardRef<
  HTMLButtonElement,
  ComposerStopButtonProps
>(function ComposerStopButton(
  { className, disabled, loading = false, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      data-chat-composer-stop
      className={cn(
        "duration-quick ease-ui size-control-sm focus-visible:ring-ring focus-visible:ring-offset-canvas inline-flex shrink-0 items-center justify-center rounded-full border-0 bg-transparent p-0 transition-opacity outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-45",
        className,
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <LoaderCircle
          aria-hidden
          className="text-button-primary size-icon-sm animate-spin"
        />
      ) : (
        <span
          aria-hidden
          className="composer-stop-indicator"
          data-chat-composer-stop-indicator
        />
      )}
    </button>
  );
});
