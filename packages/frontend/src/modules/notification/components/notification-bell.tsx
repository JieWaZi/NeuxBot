import { useQuery } from "@tanstack/react-query";
import { Bell, LoaderCircle, WifiOff } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { notificationSnapshotOptions } from "../api/notification-queries";

/** Bell Toggle 的摇铃反馈适配为导航，计数始终来自完整服务端摘要。 */
export function NotificationBell({
  onOpen,
  connected,
  active = false,
}: {
  onOpen: () => void;
  connected: boolean;
  active?: boolean;
}) {
  const { t } = useTranslation("notification");
  const snapshot = useQuery(notificationSnapshotOptions());
  const icon = useRef<SVGSVGElement>(null);
  const waves = useRef<SVGSVGElement>(null);
  const animations = useRef<Animation[]>([]);
  const reducedMotion = useReducedMotion();
  const pressTransition = useMemo(
    () => readLayoutMotionTokens("--duration-instant"),
    [],
  );
  const cancelRing = useCallback(() => {
    for (const animation of animations.current) animation.cancel();
    animations.current = [];
  }, []);
  const ring = useCallback(() => {
    cancelRing();
    if (
      !icon.current?.animate ||
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    )
      return;
    const { duration, ease } = readLayoutMotionTokens("--duration-slow");
    const timing = {
      duration: duration * 1000,
      easing: ease ? `cubic-bezier(${ease.join(",")})` : "ease-out",
    };
    animations.current.push(
      icon.current.animate(
        [
          { transform: "rotate(0deg)" },
          { transform: "rotate(-18deg)" },
          { transform: "rotate(14deg)" },
          { transform: "rotate(-8deg)" },
          { transform: "rotate(0deg)" },
        ],
        timing,
      ),
    );
    if (waves.current?.animate) {
      animations.current.push(
        waves.current.animate(
          [
            { opacity: 0, transform: "scale(0.75)" },
            { opacity: 0.8, transform: "scale(1)" },
            { opacity: 0, transform: "scale(1.2)" },
          ],
          timing,
        ),
      );
    }
  }, [cancelRing]);
  useEffect(() => {
    window.addEventListener("ally:notification-arrived", ring);
    return () => {
      window.removeEventListener("ally:notification-arrived", ring);
      cancelRing();
    };
  }, [cancelRing, ring]);
  useEffect(() => {
    if (reducedMotion) cancelRing();
  }, [cancelRing, reducedMotion]);
  const count = snapshot.data?.totalCount;
  const label =
    count === undefined
      ? t(snapshot.isError ? "unavailable" : "loading")
      : !connected
        ? t("stale", { count })
        : t("open", { count });
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          asChild
          size="icon"
          shape="capsule"
          variant="ghost"
          className="relative"
        >
          <motion.button
            type="button"
            onClick={() => {
              ring();
              onOpen();
            }}
            whileTap={reducedMotion ? undefined : { scale: 0.97 }}
            transition={reducedMotion ? { duration: 0 } : pressTransition}
            aria-label={label}
            aria-busy={snapshot.isPending}
            aria-current={active ? "page" : undefined}
            data-desktop-no-drag-region
            data-motion="view"
          >
            <span className="size-control-md relative flex shrink-0 items-center justify-center">
              <Bell
                ref={icon}
                strokeWidth={1.5}
                aria-hidden="true"
                className="origin-top"
              />
              <svg
                ref={waves}
                viewBox="0 0 36 36"
                fill="none"
                stroke="currentColor"
                strokeWidth={1.5}
                strokeLinecap="round"
                data-decoration
                aria-hidden="true"
                className="pointer-events-none absolute inset-0 size-full opacity-0"
              >
                <path d="M10 11Q5 18 10 25M26 11Q31 18 26 25" />
              </svg>
              {count !== undefined && count > 0 ? (
                <span
                  aria-hidden="true"
                  className="bg-accent text-primary-foreground type-caption absolute top-0 right-0 min-w-4 rounded-full px-1 font-medium tabular-nums"
                >
                  {count > 99 ? "99+" : count}
                </span>
              ) : null}
              {count === undefined ? (
                snapshot.isError ? (
                  <WifiOff
                    aria-hidden="true"
                    className="absolute top-0 right-0"
                  />
                ) : (
                  <LoaderCircle
                    aria-hidden="true"
                    className="absolute top-0 right-0 motion-safe:animate-spin"
                  />
                )
              ) : !connected ? (
                <WifiOff
                  aria-hidden="true"
                  className="text-content-secondary absolute bottom-0 left-0"
                />
              ) : null}
            </span>
          </motion.button>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
