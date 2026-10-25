import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent,
} from "react";
import { AudioLines, FileText, Plus } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { CardFooter } from "@/shared/ui/card";
import { useTranslation } from "react-i18next";

import type { CompanionActivity } from "@/shared/companion/activity";
import {
  companionAppearance,
  companionAvatar,
} from "@/shared/companion/appearance";
import { CompanionAvatar } from "@/shared/companion/companion-avatar";

import type { Expertise } from "../model/types";

import { useWakerShowcaseMotion } from "./use-waker-showcase-motion";

import { WakerShowcaseShell } from "./waker-showcase-shell";

import "./waker-showcase-card.css";

export type WakerShowcasePhase = "opening" | "open" | "closing" | "closed";
export type WakerShowcaseOrigin = {
  height: number;
  left: number;
  top: number;
  width: number;
};

const clamp = (value: number, min = 0, max = 100) =>
  Math.min(max, Math.max(min, value));
const round = (value: number) => Math.round(value * 100) / 100;

export function WakerShowcaseCard({
  activity = "idle",
  avatar,
  bio,
  roleName,
  workStyle = [],
  onStartChat,
  onOpenDetail,
  connectorCount,
  countsError = false,
  countsPending = false,
  expertise = [],
  name,
  onMotionEnd,
  origin,
  phase = "open",
  skillCount,
}: {
  activity?: CompanionActivity;
  avatar?: string;
  bio?: string;
  roleName?: string;
  workStyle?: string[];
  onStartChat?: () => void;
  onOpenDetail?: () => void;
  connectorCount: number;
  countsError?: boolean;
  countsPending?: boolean;
  expertise?: Expertise[];
  name: string;
  onMotionEnd?: () => void;
  origin?: WakerShowcaseOrigin | null;
  phase?: WakerShowcasePhase;
  skillCount: number;
}) {
  const { t } = useTranslation("waker");
  const resolvedAvatar = companionAvatar(avatar, name);
  const appearance = companionAppearance(resolvedAvatar);
  const titles = expertise
    .map((item) => item.title?.trim())
    .filter((title): title is string => Boolean(title));
  const traits = workStyle
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 4);
  const status =
    activity === "waiting"
      ? "waiting"
      : [
            "starting",
            "thinking",
            "searching",
            "working",
            "connecting",
            "responding",
          ].includes(activity)
        ? "working"
        : "ready";
  const cardRef = useRef<HTMLElement>(null);
  const [reveal] = useState(() => phase === "opening");
  const motion = useWakerShowcaseMotion(
    cardRef,
    phase === "closing" || phase === "closed",
  );

  useEffect(() => {
    const node = cardRef.current;
    if (!node) return;
    node.style.setProperty("--card-glow", appearance.glow);
  }, [appearance.glow]);

  useLayoutEffect(() => {
    const node = cardRef.current;
    if (!node) return;
    applyFlight(node, origin, Boolean(origin));
  }, [origin]);

  const move = (event: PointerEvent<HTMLElement>) => {
    const node = cardRef.current;
    if (!node || phase !== "open") return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    if (
      event.target instanceof Element &&
      event.target.closest(".ally-holo-card__figure")
    ) {
      motion.current?.();
      return;
    }
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const percent = {
      x: clamp(round((100 / rect.width) * (event.clientX - rect.left))),
      y: clamp(round((100 / rect.height) * (event.clientY - rect.top))),
    };
    motion.current?.({
      rotateX: round(-(percent.x - 50) / 3.5),
      rotateY: round((percent.y - 50) / 3.5),
    });
  };

  const leave = () => {
    if (phase === "open") motion.current?.();
  };

  return (
    <article
      ref={cardRef}
      className="ally-holo-card"
      data-phase={phase}
      data-reveal={reveal}
      onAnimationEnd={(event) => {
        if (
          event.target instanceof HTMLElement &&
          event.target.classList.contains("ally-holo-card__translater")
        ) {
          onMotionEnd?.();
        }
      }}
      onPointerEnter={move}
      onPointerLeave={leave}
      onPointerMove={move}
    >
      <div className="ally-holo-card__translater">
        <div className="ally-holo-card__tilter">
          <div className="ally-holo-card__rotator">
            <div className="ally-holo-card__front">
              <WakerShowcaseShell />
              <span
                className="ally-holo-card__tab type-caption"
                aria-hidden="true"
              >
                {t("detail.showcaseAgent")}
              </span>
              <div className="ally-holo-card__frame">
                <div className="ally-holo-card__art">
                  <span
                    className="ally-holo-card__status type-caption"
                    data-status={status}
                  >
                    <span aria-hidden="true" />
                    {t(`detail.showcaseStatus.${status}`)}
                  </span>
                  <span className="ally-holo-card__orbit" aria-hidden="true" />
                  <span
                    className="ally-holo-card__serial type-caption"
                    aria-hidden="true"
                  >
                    {t("detail.showcaseBack")}
                  </span>
                  <div className="ally-holo-card__figure">
                    <CompanionAvatar
                      activity={activity}
                      avatar={resolvedAvatar}
                      interactiveLabel={t("detail.showcaseTouch", { name })}
                      interactiveTitle={t("detail.showcaseTouch", { name })}
                    />
                  </div>
                </div>
                <div
                  className="ally-holo-card__plate"
                  data-traits={traits.length > 0}
                >
                  <div className="ally-holo-card__profile">
                    <h2
                      className="type-display truncate font-semibold"
                      title={name}
                    >
                      {name}
                    </h2>
                    <p
                      className="type-body text-muted-foreground truncate"
                      title={roleName}
                    >
                      {roleName?.trim() || t("detail.showcaseCompanion")}
                    </p>
                    {bio?.trim() ? (
                      <p className="ally-holo-card__bio type-body" title={bio}>
                        <span>{bio}</span>
                      </p>
                    ) : null}
                    <ul
                      className="ally-holo-card__abilities type-ui"
                      aria-label={t("detail.showcaseExpertise")}
                    >
                      {titles.slice(0, 6).map((title, index) => (
                        <li key={`${index}-${title}`} title={title}>
                          <span>{title}</span>
                        </li>
                      ))}
                      {titles.length > 6 ? (
                        <li title={titles.slice(6).join(" · ")}>
                          +{titles.length - 6}
                        </li>
                      ) : null}
                    </ul>
                    {titles.length === 0 ? (
                      <p className="type-caption text-muted-foreground">
                        {t("detail.showcaseExpertiseEmpty")}
                      </p>
                    ) : null}
                  </div>
                  {traits.length > 0 ? (
                    <aside
                      className="ally-holo-card__traits type-caption"
                      aria-label={t("detail.showcaseStyle")}
                    >
                      {traits.map((trait, index) => (
                        <span key={`${index}-${trait}`} title={trait}>
                          {trait}
                        </span>
                      ))}
                    </aside>
                  ) : null}
                </div>
                <CardFooter className="ally-holo-card__footer px-0">
                  <div className="ally-holo-card__ready">
                    <span className="ally-holo-card__voice" aria-hidden="true">
                      <AudioLines />
                    </span>
                    <div className="min-w-0">
                      <p className="type-body font-medium">
                        {t(`detail.showcaseFooter.${status}`)}
                      </p>
                      <p
                        aria-busy={countsPending || undefined}
                        className="text-muted-foreground type-caption mt-1"
                        role="status"
                      >
                        {countsPending
                          ? t("detail.showcaseCountsLoading")
                          : countsError
                            ? t("detail.showcaseCountsError")
                            : t("detail.showcaseCounts", {
                                connectors: connectorCount,
                                skills: skillCount,
                              })}
                      </p>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center justify-end gap-2">
                    {onOpenDetail ? (
                      <Button
                        size="md"
                        shape="capsule"
                        variant="secondary"
                        onClick={onOpenDetail}
                        disabled={phase === "closing" || phase === "closed"}
                      >
                        <FileText aria-hidden="true" />
                        {t("common:sceneTabs.detail")}
                      </Button>
                    ) : null}
                    {onStartChat ? (
                      <Button
                        size="md"
                        shape="capsule"
                        onClick={onStartChat}
                        disabled={phase === "closing" || phase === "closed"}
                      >
                        <Plus aria-hidden="true" />
                        {t("detail.showcaseStartChat")}
                      </Button>
                    ) : null}
                  </div>
                </CardFooter>
              </div>
            </div>
            <div className="ally-holo-card__back" aria-hidden="true">
              <strong className="ally-holo-card__back-mark type-title font-semibold">
                {t("detail.showcaseBack")}
              </strong>
            </div>
          </div>
        </div>
      </div>
    </article>
  );
}

function applyFlight(
  node: HTMLElement,
  origin: WakerShowcaseOrigin | null | undefined,
  flying: boolean,
) {
  if (!origin || !flying) {
    node.style.setProperty("--from-x", "0px");
    node.style.setProperty("--from-y", "0px");
    node.style.setProperty("--from-scale", "1");
    return;
  }
  const destWidth = node.offsetWidth || 320;
  const originCenterX = origin.left + origin.width / 2;
  const originCenterY = origin.top + origin.height / 2;
  node.style.setProperty(
    "--from-x",
    `${originCenterX - window.innerWidth / 2}px`,
  );
  node.style.setProperty(
    "--from-y",
    `${originCenterY - window.innerHeight / 2}px`,
  );
  node.style.setProperty(
    "--from-scale",
    `${clamp(origin.width / destWidth, 0.08, 1)}`,
  );
}
