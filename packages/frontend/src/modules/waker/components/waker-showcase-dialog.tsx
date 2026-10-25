import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { wakerCompanionActivity } from "@/shared/companion/activity";
import { Dialog, DialogContent } from "@/shared/ui/dialog";

import {
  listWakerConnectorsOptions,
  listWakerSkillsOptions,
} from "../api/waker-queries";
import type { Waker } from "../model/types";
import {
  WakerShowcaseCard,
  type WakerShowcaseOrigin,
  type WakerShowcasePhase,
} from "./waker-showcase-card";

export function WakerShowcaseDialog({
  onOpenChange,
  onStartChat,
  onOpenDetail,
  open,
  originRect,
  waker,
}: {
  onOpenChange: (open: boolean) => void;
  onStartChat?: (wakerId: string) => void;
  onOpenDetail?: (wakerId: string) => void;
  open: boolean;
  origin?: HTMLElement | null;
  originRect?: WakerShowcaseOrigin | null;
  waker: Waker | null;
}) {
  const { t } = useTranslation("waker");
  const { t: tCommon } = useTranslation("common");
  const wakerId = waker?.id ?? "";
  const name = waker?.name?.trim() || tCommon("partnerNavigation.partner");
  const [shown, setShown] = useState(waker);
  if (waker && shown !== waker) setShown(waker);
  const [phase, setPhase] = useState<WakerShowcasePhase>("opening");
  const [sessionId, setSessionId] = useState("");
  const skillsQuery = useQuery({
    ...listWakerSkillsOptions({ path: { wakerId } }),
    enabled: open && Boolean(wakerId),
  });
  const connectorsQuery = useQuery({
    ...listWakerConnectorsOptions({ path: { wakerId } }),
    enabled: open && Boolean(wakerId),
  });

  if (open && waker?.id && waker.id !== sessionId) {
    setSessionId(waker.id);
    setPhase(prefersReducedMotion() ? "open" : "opening");
  }
  if (!open && phase !== "closing" && sessionId) {
    setSessionId("");
  }

  const shownName = shown?.name?.trim() || name;

  return (
    <Dialog
      open={(open && phase !== "closed") || phase === "closing"}
      onOpenChange={(nextOpen) => {
        if (nextOpen) {
          onOpenChange(true);
          return;
        }
        if (phase === "closing" || phase === "closed") return;
        if (prefersReducedMotion()) {
          setPhase("closed");
          onOpenChange(false);
          return;
        }
        setPhase("closing");
      }}
    >
      {shown ? (
        <DialogContent
          closing={phase === "closing" || phase === "closed"}
          layout="stage"
          title={t("detail.showcaseTitle", { name: shownName })}
        >
          <WakerShowcaseCard
            activity={wakerCompanionActivity(shown.activityState)}
            avatar={shown.avatar}
            bio={shown.bio}
            roleName={shown.roleName}
            workStyle={shown.workStyle}
            onOpenDetail={
              shown.id && onOpenDetail
                ? () => {
                    if (!shown.id) return;
                    setPhase("closed");
                    onOpenChange(false);
                    onOpenDetail(shown.id);
                  }
                : undefined
            }
            onStartChat={
              shown.id && onStartChat
                ? () => {
                    if (!shown.id) return;
                    setPhase("closed");
                    onOpenChange(false);
                    onStartChat(shown.id);
                  }
                : undefined
            }
            connectorCount={countConnectors(connectorsQuery.data)}
            countsError={skillsQuery.isError || connectorsQuery.isError}
            countsPending={
              (skillsQuery.isPending && skillsQuery.fetchStatus !== "idle") ||
              (connectorsQuery.isPending &&
                connectorsQuery.fetchStatus !== "idle")
            }
            expertise={shown.expertise}
            name={shownName}
            origin={originRect}
            phase={phase}
            skillCount={countSkills(skillsQuery.data)}
            onMotionEnd={() => {
              if (phase === "opening") {
                setPhase("open");
                return;
              }
              if (phase !== "closing") return;
              // 保留退出终态直到卸载，避免 Presence 退出期间重播展开动画。
              setPhase("closed");
              onOpenChange(false);
            }}
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

function prefersReducedMotion() {
  return (
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false
  );
}

function countSkills(skills: Array<{ skill?: { id?: string } }> | undefined) {
  return (skills ?? []).filter((item) => item.skill?.id).length;
}

function countConnectors(
  connectors: Array<{ connector?: { id?: string } }> | undefined,
) {
  return (connectors ?? []).filter((item) => item.connector?.id).length;
}
