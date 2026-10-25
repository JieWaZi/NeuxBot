import {
  CircleCheck,
  Info,
  LoaderCircle,
  OctagonX,
  TriangleAlert,
} from "lucide-react";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  type CSSProperties,
} from "react";
import {
  Toaster as Sonner,
  type ToasterProps,
  type ExternalToast,
  toast as sonnerToast,
  useSonner,
} from "sonner";

import { cn } from "@/shared/lib/cn";
import { useTheme } from "@/shared/theme";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";

type ToasterStyle = CSSProperties & {
  "--border-radius": string;
  "--normal-bg": string;
  "--normal-border": string;
  "--normal-text": string;
};

const toasterStyle: ToasterStyle = {
  "--border-radius": "var(--radius-lg)",
  "--normal-bg": "var(--surface-elevated)",
  "--normal-border": "var(--border)",
  "--normal-text": "var(--foreground)",
};

const defaultDuration = 4000;
const ToastTimingContext = createContext({
  duration: defaultDuration,
  expand: false,
});

/** 倒计时只呈现 Sonner 的剩余时长，关闭与滑动仍由原组件负责。 */
function ToastMessage({
  message,
  duration,
}: {
  message: Parameters<typeof sonnerToast>[0];
  duration?: number;
}) {
  const timing = useContext(ToastTimingContext);
  const fuseRef = useRef<HTMLSpanElement>(null);
  const lifetime = duration ?? timing.duration;

  useEffect(() => {
    const fuse = fuseRef.current;
    const card = fuse?.closest<HTMLElement>("[data-sonner-toast]");
    const toaster = card?.closest<HTMLElement>("[data-sonner-toaster]");
    if (!fuse?.animate || !card || !toaster) return;

    const animation = fuse.animate(
      [{ transform: "scaleX(1)" }, { transform: "scaleX(0)" }],
      { duration: lifetime, easing: "linear", fill: "forwards" },
    );
    let hovering = toaster.matches(":hover");
    let interacting = false;
    const sync = () => {
      const expanded = !timing.expand && card.dataset.expanded === "true";
      if (document.hidden || hovering || interacting || expanded) {
        animation.pause();
      } else if (animation.playState === "paused") {
        animation.play();
      }
    };
    const enter = () => {
      hovering = true;
      sync();
    };
    const leave = () => {
      hovering = false;
      sync();
    };
    const down = () => {
      interacting = true;
      sync();
    };
    const up = () => {
      interacting = false;
      sync();
    };
    const observer = new MutationObserver(sync);
    observer.observe(card, {
      attributes: true,
      attributeFilter: ["data-expanded"],
    });
    toaster.addEventListener("mouseenter", enter);
    toaster.addEventListener("mouseleave", leave);
    toaster.addEventListener("pointerdown", down);
    toaster.addEventListener("pointerup", up);
    document.addEventListener("visibilitychange", sync);
    sync();
    return () => {
      observer.disconnect();
      toaster.removeEventListener("mouseenter", enter);
      toaster.removeEventListener("mouseleave", leave);
      toaster.removeEventListener("pointerdown", down);
      toaster.removeEventListener("pointerup", up);
      document.removeEventListener("visibilitychange", sync);
      animation.cancel();
    };
  }, [lifetime, timing.expand]);

  return (
    <>
      <span className="block min-w-0 truncate">
        {typeof message === "function" ? message() : message}
      </span>
      {Number.isFinite(lifetime) && lifetime > 0 ? (
        <span
          ref={fuseRef}
          aria-hidden="true"
          data-toast-fuse=""
          className="via-accent pointer-events-none absolute inset-x-0 bottom-0 h-0.5 origin-left bg-linear-to-r from-transparent to-transparent motion-reduce:hidden"
        />
      ) : null}
    </>
  );
}

function withCountdown(
  message: Parameters<typeof sonnerToast>[0],
  options?: ExternalToast,
) {
  return <ToastMessage message={message} duration={options?.duration} />;
}

/** shadcn/ui 官方 Sonner 包装，适配 NeuxBot 的主题与尺寸 Token。 */
export function Toaster({
  offset = {
    top: "calc(var(--shell-titlebar-height) + var(--shell-inset) + 8px)",
    right: "calc(var(--shell-inset) + 12px)",
  },
  position = "top-right",
  toastOptions,
  duration = defaultDuration,
  expand = false,
  ...props
}: ToasterProps) {
  const { resolvedTheme } = useTheme();
  const { toasts } = useSonner();

  return (
    <ToastTimingContext.Provider
      value={{ duration: toastOptions?.duration ?? duration, expand }}
    >
      <NativeOverlayPresence active={toasts.length > 0} />
      <Sonner
        className="toaster group"
        icons={{
          error: (
            <OctagonX
              className="text-destructive size-icon-md"
              strokeWidth={1.5}
            />
          ),
          info: (
            <Info
              className="text-muted-foreground size-icon-md"
              strokeWidth={1.5}
            />
          ),
          loading: (
            <LoaderCircle
              className="text-muted-foreground size-icon-md animate-spin"
              strokeWidth={1.5}
            />
          ),
          success: (
            <CircleCheck
              className="text-success size-icon-md"
              strokeWidth={1.5}
            />
          ),
          warning: (
            <TriangleAlert
              className="text-warning size-icon-md"
              strokeWidth={1.5}
            />
          ),
        }}
        position={position}
        duration={duration}
        expand={expand}
        offset={offset}
        richColors={false}
        style={toasterStyle}
        theme={resolvedTheme}
        toastOptions={{
          ...toastOptions,
          closeButton: false,
          classNames: {
            ...toastOptions?.classNames,
            content: cn(
              "min-w-0! flex-1! gap-0.5! p-0!",
              toastOptions?.classNames?.content,
            ),
            icon: cn(
              "m-0! size-icon-lg! justify-center! [&_svg]:m-0!",
              toastOptions?.classNames?.icon,
            ),
            title: cn(
              "type-body! min-w-0! font-medium!",
              toastOptions?.classNames?.title,
            ),
            description: cn(
              "type-body! truncate! text-muted-foreground!",
              toastOptions?.classNames?.description,
            ),
            actionButton: cn(
              "m-0! h-button-sm! rounded-sm! px-3! type-ui! focus-visible:ring-2 focus-visible:ring-ring",
              toastOptions?.classNames?.actionButton,
            ),
            cancelButton: cn(
              "m-0! h-button-sm! rounded-sm! px-3! type-ui! focus-visible:ring-2 focus-visible:ring-ring",
              toastOptions?.classNames?.cancelButton,
            ),
            toast: cn(
              "items-center! gap-2! rounded-lg! border-0! bg-surface-elevated! p-4! type-body! text-foreground! shadow-sm! transition-[transform,opacity,height]! duration-standard! ease-ui! focus-visible:ring-2 focus-visible:ring-ring",
              toastOptions?.classNames?.toast,
            ),
          },
        }}
        {...props}
      />
    </ToastTimingContext.Provider>
  );
}

/** 普通通知显示倒计时；错误自动关闭，审批等显式常驻通知保留原行为。 */
export const toast = Object.assign(
  (message: Parameters<typeof sonnerToast>[0], options?: ExternalToast) =>
    sonnerToast(withCountdown(message, options), options),
  sonnerToast,
  {
    error: (...[message, options]: Parameters<typeof sonnerToast.error>) =>
      sonnerToast.error(
        withCountdown(message, {
          duration:
            Number.isFinite(options?.duration) && (options?.duration ?? 0) > 0
              ? options?.duration
              : defaultDuration,
        }),
        {
          ...options,
          duration:
            Number.isFinite(options?.duration) && (options?.duration ?? 0) > 0
              ? options?.duration
              : defaultDuration,
          closeButton: false,
        },
      ),
    success: (
      message: Parameters<typeof sonnerToast>[0],
      options?: ExternalToast,
    ) => sonnerToast.success(withCountdown(message, options), options),
    info: (
      message: Parameters<typeof sonnerToast>[0],
      options?: ExternalToast,
    ) => sonnerToast.info(withCountdown(message, options), options),
    warning: (
      message: Parameters<typeof sonnerToast>[0],
      options?: ExternalToast,
    ) => sonnerToast.warning(withCountdown(message, options), options),
    message: (
      message: Parameters<typeof sonnerToast>[0],
      options?: ExternalToast,
    ) => sonnerToast.message(withCountdown(message, options), options),
  },
);
