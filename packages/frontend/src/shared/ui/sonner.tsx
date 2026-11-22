import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  CircleCheck,
  Info,
  OctagonX,
  X,
} from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { Toaster as Sonner, toast as sonnerToast } from "sonner";
import { useTranslation } from "react-i18next";

import { useTheme } from "@/shared/theme";
import { Button } from "@/shared/ui/button";
import { registerNativeOverlayRegion } from "@/shared/ui/native-overlay";
import {
  closeNotification,
  finishNotificationExit,
  notificationDuration,
  notificationExitDuration,
  useNotifications,
} from "@/shared/ui/notification-store";

export { toast } from "@/shared/ui/notification-store";

const pageSize = 4;
const cardGap = 8;
const exitTiming: CSSProperties & { "--notification-exit-duration": string } = {
  "--notification-exit-duration": `${notificationExitDuration}ms`,
};
const icons = {
  error: (
    <OctagonX className="text-destructive size-icon-md" strokeWidth={1.5} />
  ),
  info: (
    <Info className="text-muted-foreground size-icon-md" strokeWidth={1.5} />
  ),
  success: (
    <CircleCheck className="text-success size-icon-md" strokeWidth={1.5} />
  ),
};

type Notifications = ReturnType<typeof useNotifications>;
const ExpandedContext = createContext(false);

function Countdown({ expiresAt }: { expiresAt: number }) {
  const fuse = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const animation = fuse.current?.animate?.(
      [{ transform: "scaleX(1)" }, { transform: "scaleX(0)" }],
      { duration: notificationDuration, easing: "linear", fill: "forwards" },
    );
    if (!animation) return;
    animation.currentTime = Math.max(
      0,
      notificationDuration - (expiresAt - performance.now()),
    );
    return () => animation.cancel();
  }, [expiresAt]);

  return (
    <span
      ref={fuse}
      aria-hidden="true"
      data-toast-fuse=""
      className="via-accent pointer-events-none absolute inset-x-0 bottom-0 h-0.5 origin-left bg-linear-to-r from-transparent to-transparent motion-reduce:hidden"
    />
  );
}

function NotificationCard({
  notice,
  slot,
  onMeasure,
}: {
  notice?: Notifications[number];
  slot: number;
  onMeasure: (slot: number, height: number) => void;
}) {
  const { t } = useTranslation("notification");
  const expanded = useContext(ExpandedContext);
  const content = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const item = content.current?.closest<HTMLElement>("[data-sonner-toast]");
    if (!item) return;
    item.toggleAttribute("data-notification-exiting", notice?.exiting === true);
    item.inert = !notice || notice.exiting === true || (slot > 0 && !expanded);
    item.setAttribute("aria-hidden", String(item.inert));
    item.tabIndex = -1;
    if (notice?.exiting) {
      const finish = (event: AnimationEvent) => {
        if (
          event.target === item &&
          event.animationName === "notification-card-exit"
        )
          finishNotificationExit(notice.id);
      };
      item.addEventListener("animationend", finish);
      return () => item.removeEventListener("animationend", finish);
    }
  }, [expanded, notice, slot]);
  useLayoutEffect(() => {
    const card = content.current;
    if (!card) return;
    const reportHeight = () => onMeasure(slot, card.offsetHeight);
    reportHeight();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(reportHeight);
    observer.observe(card);
    return () => observer.disconnect();
  }, [notice, onMeasure, slot]);
  if (!notice) return <div ref={content} hidden />;
  return (
    <div
      ref={content}
      className="text-foreground relative flex flex-col gap-2 p-4"
    >
      <div className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-2 pr-5">
        <span
          aria-hidden="true"
          className="size-icon-lg flex items-center justify-center"
        >
          {notice.icon ?? icons[notice.kind]}
        </span>
        <div
          role={notice.kind === "error" ? "alert" : "status"}
          className="min-w-0 space-y-0.5 [overflow-wrap:anywhere]"
        >
          <div className="type-body font-medium">{notice.title}</div>
          {notice.description && (
            <div className="text-muted-foreground type-body">
              {notice.description}
            </div>
          )}
        </div>
      </div>
      <Button
        data-notification-close=""
        type="button"
        size="icon"
        variant="ghost"
        shape="circle"
        className="absolute top-1 right-1"
        aria-label={
          typeof notice.title === "string"
            ? t("inApp.closeNamed", { title: notice.title })
            : t("inApp.close")
        }
        onClick={() => closeNotification(notice.id)}
      >
        <X aria-hidden="true" />
      </Button>
      {notice.action && (
        <div className="flex justify-end">
          <Button type="button" size="sm" onClick={notice.action.onClick}>
            {notice.action.label}
          </Button>
        </div>
      )}
      {notice.expiresAt !== undefined && (
        <Countdown expiresAt={notice.expiresAt} />
      )}
    </div>
  );
}

/** Sonner 拥有卡片的位移与层叠，当前页只投影四个有固定顺序的展示位置。 */
function NotificationPage({
  notices,
  expanded,
}: {
  notices: Notifications;
  expanded: boolean;
}) {
  const channel = useId();
  const { resolvedTheme } = useTheme();
  const host = useRef<HTMLDivElement>(null);
  const projected = useRef<Notifications>([]);

  const heights = useRef(Array<number>(pageSize).fill(0));
  const measure = useCallback((slot: number, height: number) => {
    const element = host.current;
    if (!element || heights.current[slot] === height) return;
    heights.current[slot] = height;
    let offset = 0;
    for (const [index, cardHeight] of heights.current.entries()) {
      element.style.setProperty(
        `--notification-offset-${index}`,
        `${offset}px`,
      );
      element.style.setProperty(
        `--notification-height-${index}`,
        `${cardHeight}px`,
      );
      if (cardHeight) offset += cardHeight + cardGap;
    }
    element.style.setProperty("--notification-page-height", `${offset}px`);
  }, []);

  useEffect(() => {
    for (let slot = pageSize - 1; slot >= 0; slot -= 1) {
      const notice = notices[slot];
      if (projected.current.length && projected.current[slot] === notice)
        continue;
      const id = `${channel}:${slot}`;
      const style: CSSProperties & {
        "--notification-slot-offset": string;
        "--notification-slot-height": string;
      } = {
        "--notification-slot-offset": `var(--notification-offset-${slot}, 0px)`,
        "--notification-slot-height": `var(--notification-height-${slot}, auto)`,
      };
      sonnerToast.custom(
        () => (
          <NotificationCard
            key={notice?.id}
            notice={notice}
            slot={slot}
            onMeasure={measure}
          />
        ),
        {
          id,
          toasterId: channel,
          duration: Infinity,
          dismissible: false,
          className: notice ? "notification-sonner-card" : "hidden!",
          style,
        },
      );
    }
    projected.current = notices;
  }, [channel, measure, notices]);
  useEffect(
    () => () => {
      projected.current = [];
      for (let slot = 0; slot < pageSize; slot += 1)
        sonnerToast.dismiss(`${channel}:${slot}`);
    },
    [channel],
  );

  return (
    <div
      ref={host}
      data-notification-scroll=""
      className="min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain"
    >
      <ExpandedContext.Provider value={expanded}>
        <Sonner
          id={channel}
          className="notification-sonner-list"
          position="top-right"
          theme={resolvedTheme}
          expand={expanded}
          visibleToasts={pageSize}
          gap={cardGap}
          hotkey={[]}
          customAriaLabel=""
        />
      </ExpandedContext.Provider>
    </div>
  );
}

function NotificationStack({ notices }: { notices: Notifications }) {
  const { t } = useTranslation("notification");
  const listId = useId();
  const viewport = useRef<HTMLElement>(null);
  const [hovered, setHovered] = useState(false);
  const [pinned, setPinned] = useState(false);
  const [page, setPage] = useState(0);
  const [previousNotices, setPreviousNotices] = useState(notices);
  if (previousNotices !== notices) {
    setPreviousNotices(notices);
    if (
      page > 0 &&
      notices.some(
        (notice, index) =>
          index < pageSize &&
          notice.expiresAt !== undefined &&
          !notice.exiting &&
          !previousNotices.includes(notice),
      )
    )
      setPage(0);
  }
  const lastPage = Math.ceil(notices.length / pageSize) - 1;
  const currentPage = Math.min(page, lastPage);
  const stacked = notices.length > 1;
  const showExpanded = stacked && (hovered || pinned);
  const start = currentPage * pageSize;
  const visible = notices.slice(start, start + pageSize);
  useLayoutEffect(() => {
    const region = viewport.current;
    if (region) return registerNativeOverlayRegion(region);
  }, []);
  useLayoutEffect(() => {
    const scroll = viewport.current?.querySelector<HTMLElement>(
      "[data-notification-scroll]",
    );
    if (scroll) scroll.scrollTop = 0;
  }, [currentPage]);
  useEffect(() => {
    if (!pinned) return;
    const closeFromOutside = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        !viewport.current?.contains(event.target)
      ) {
        setHovered(false);
        setPinned(false);
      }
    };
    document.addEventListener("pointerdown", closeFromOutside, true);
    return () =>
      document.removeEventListener("pointerdown", closeFromOutside, true);
  }, [pinned]);
  useEffect(() => {
    if (showExpanded || page === 0) return;
    const list = viewport.current?.querySelector("[data-sonner-toaster]");
    const transitions = list?.getAnimations?.() ?? [];
    let active = true;
    void Promise.allSettled(
      transitions.map((transition) => transition.finished),
    ).then(() => {
      if (active) setPage(0);
    });
    return () => {
      active = false;
    };
  }, [page, showExpanded]);
  useEffect(() => {
    const focusNotifications = (event: KeyboardEvent) => {
      if (
        !event.altKey ||
        event.shiftKey ||
        event.ctrlKey ||
        event.metaKey ||
        event.code !== "KeyT"
      )
        return;
      event.preventDefault();
      setPinned(true);
      const frontClose = viewport.current?.querySelector<HTMLButtonElement>(
        '[data-front="true"] [data-notification-close]',
      );
      (
        frontClose ??
        viewport.current?.querySelector<HTMLButtonElement>(
          "[data-notification-toggle]",
        )
      )?.focus();
    };
    window.addEventListener("keydown", focusNotifications);
    return () => window.removeEventListener("keydown", focusNotifications);
  }, []);

  const changePage = (nextPage: number) => {
    if (nextPage < 0 || nextPage > lastPage) return;
    setPinned(true);
    setPage(nextPage);
  };

  return (
    <section
      ref={viewport}
      aria-label={t("inApp.title")}
      data-notification-viewport=""
      style={exitTiming}
      className="fixed top-[calc(var(--shell-titlebar-height)_+_var(--shell-inset)_+_8px)] right-[calc(var(--shell-inset)_+_12px)] z-100 flex max-h-[calc(100dvh_-_var(--shell-titlebar-height)_-_var(--shell-inset)_-_24px)] w-96 max-w-[calc(100vw_-_24px_-_var(--shell-inset))] flex-col"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onBlurCapture={(event) => {
        if (
          event.relatedTarget &&
          !event.currentTarget.contains(event.relatedTarget)
        ) {
          setPinned(false);
        }
      }}
      onKeyDown={(event) => {
        if (event.key !== "Escape" || !showExpanded) return;
        event.stopPropagation();
        event.currentTarget
          .querySelector<HTMLButtonElement>("[data-notification-toggle]")
          ?.focus({ preventScroll: true });
        setPinned(false);
        setHovered(false);
      }}
    >
      <div
        id={listId}
        className="flex min-h-0 flex-col"
        data-notification-expanded={showExpanded}
        data-notification-stacked={stacked}
      >
        <NotificationPage notices={visible} expanded={showExpanded} />
      </div>
      {stacked && (
        <Button
          data-notification-toggle=""
          type="button"
          size="sm"
          variant="ghost"
          className="bg-surface-elevated self-end [@media(hover:hover)]:sr-only [@media(hover:hover)]:focus:not-sr-only"
          aria-controls={listId}
          aria-expanded={showExpanded}
          onClick={() => {
            setPinned(!showExpanded);
            setHovered(false);
          }}
        >
          {showExpanded ? (
            <ChevronUp aria-hidden="true" />
          ) : (
            <ChevronDown aria-hidden="true" />
          )}
          {showExpanded
            ? t("inApp.collapse")
            : t("inApp.more", { count: notices.length - 1 })}
        </Button>
      )}
      {lastPage > 0 && (
        <div
          className="notification-pagination"
          data-expanded={showExpanded}
          aria-hidden={!showExpanded}
          inert={!showExpanded}
          onPointerDown={(event) => {
            if (event.button === 0) setPinned(true);
          }}
        >
          <div className="bg-surface-elevated text-muted-foreground type-ui flex min-h-0 items-center justify-end overflow-hidden rounded-lg px-1 shadow-sm">
            <Button
              size="icon"
              variant="ghost"
              aria-label={t("inApp.previous")}
              aria-disabled={currentPage === 0}
              className="aria-disabled:opacity-45"
              onClick={() => changePage(currentPage - 1)}
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <span>
              {t("inApp.page", { page: currentPage + 1, total: lastPage + 1 })}
            </span>
            <Button
              size="icon"
              variant="ghost"
              aria-label={t("inApp.next")}
              aria-disabled={currentPage === lastPage}
              className="aria-disabled:opacity-45"
              onClick={() => changePage(currentPage + 1)}
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

/** 应用通知共用主题表面与有界的右上角列表。 */
export function Toaster() {
  const notices = useNotifications();
  if (!notices.length) return null;
  return <NotificationStack notices={notices} />;
}
