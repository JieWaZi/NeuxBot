import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowUpRight,
  ChevronRight,
  Settings,
  X,
} from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { getChannelOptions, TeamFormDialog } from "@/modules/team";
import {
  WakerDefaultModelAction,
  WakerFormDialog,
  WakerProfileSection,
} from "@/modules/waker";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { Button, buttonVariants } from "@/shared/ui/button";
import { QueryState } from "@/shared/ui/query-state";
import type { ConversationSubject } from "../api/main-conversation";

export function useMainConversationSettings(
  subject: ConversationSubject,
  visible: boolean,
) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const { t } = useTranslation("chat");
  const label = t(
    subject.subjectType === "team" ? "teamSettings" : "partnerSettings",
  );
  const close = () => {
    setOpen(false);
    trigger.current?.focus();
  };
  return {
    conversationActions: visible ? (
      <button
        ref={trigger}
        type="button"
        className={buttonVariants({ variant: "ghost", size: "icon" })}
        aria-label={label}
        title={label}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Settings />
      </button>
    ) : undefined,
    conversationPanel:
      visible && open ? (
        <SettingsPanel subject={subject} label={label} close={close} />
      ) : undefined,
  };
}

function SettingsPanel({
  subject,
  label,
  close,
}: {
  subject: ConversationSubject;
  label: string;
  close: () => void;
}) {
  const { t: common } = useTranslation("common");
  return (
    <aside
      aria-label={label}
      className="border-border bg-surface-scene motion-safe:animate-in motion-safe:slide-in-from-right-4 absolute inset-y-0 right-0 z-30 flex w-80 max-w-full flex-col border-l motion-safe:duration-200"
      onKeyDown={(event) => {
        if (event.key === "Escape" && !event.defaultPrevented) close();
      }}
    >
      <header className="flex h-11 shrink-0 items-center justify-between px-4">
        <h2 className="text-sm font-medium">{label}</h2>
        <Button
          variant="ghost"
          size="icon"
          aria-label={common("close")}
          onClick={close}
        >
          <X />
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto px-4">
        {subject.subjectType === "waker" ? (
          <PartnerSettings id={subject.subjectId} />
        ) : (
          <TeamSettings id={subject.subjectId} />
        )}
      </div>
    </aside>
  );
}

function PartnerSettings({ id }: { id: string }) {
  const { t } = useTranslation("common");
  const { t: wakerText } = useTranslation("waker");
  const [editing, setEditing] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const { t: tasksText } = useTranslation("automaticTask");
  const query = useQuery(getWakerOptions({ path: { wakerId: id } }));
  const waker = query.data;
  if (!waker)
    return (
      <QueryState
        error={query.error ?? undefined}
        onRetry={() => void query.refetch()}
      />
    );
  if (profileOpen) {
    return (
      <div className="grid gap-2">
        <Button
          variant="ghost"
          size="sm"
          className="justify-start"
          onClick={() => setProfileOpen(false)}
        >
          <ArrowLeft />
          {t("close")}
        </Button>
        <WakerProfileSection waker={waker} />
      </div>
    );
  }
  return (
    <>
      <div className="flex items-center justify-between gap-2 py-2">
        <span className="truncate text-sm font-medium">{waker.name}</span>
        <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
          {t("partnerNavigation.wakerActions.edit")}
        </Button>
      </div>
      <div className="flex items-center justify-between gap-2 py-2 text-sm">
        {wakerText("form.defaultModel")}
        <WakerDefaultModelAction wakerId={id} value={waker.defaultModel} />
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="w-full justify-between px-2"
        onClick={() => setProfileOpen(true)}
      >
        {wakerText("profile.title")}
        <ChevronRight />
      </Button>
      {(["projects", "memory", "resources"] as const).map((section) => (
        <Link
          key={section}
          to={`/wakers/${id}/${section}`}
          className="hover:bg-muted flex items-center justify-between rounded-md px-2 py-2 text-sm"
        >
          {t(`partnerNavigation.sections.${section}`)}
          <ArrowUpRight aria-hidden className="size-3.5" />
        </Link>
      ))}
      <Link
        to={`/wakers/${id}/automatic-tasks`}
        className="hover:bg-muted flex items-center justify-between rounded-md px-2 py-2 text-sm"
      >
        {tasksText("title")}
        <ArrowUpRight aria-hidden className="size-3.5" />
      </Link>
      <WakerFormDialog waker={waker} open={editing} onOpenChange={setEditing} />
    </>
  );
}

function TeamSettings({ id }: { id: string }) {
  const { t } = useTranslation("team");
  const [editing, setEditing] = useState(false);
  const query = useQuery(getChannelOptions({ path: { channelId: id } }));
  const detail = query.data;
  if (!detail)
    return (
      <QueryState
        error={query.error ?? undefined}
        onRetry={() => void query.refetch()}
      />
    );
  return (
    <>
      <div className="flex items-center justify-between gap-2 py-2">
        <span className="truncate text-sm font-medium">
          {detail.channel?.name}
        </span>
        <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
          {t("edit")}
        </Button>
      </div>
      {(detail.members ?? []).map((member) => (
        <div
          className="flex justify-between gap-2 py-2 text-sm"
          key={member.waker?.id}
        >
          <span className="truncate">{member.waker?.name}</span>
          {member.waker?.id === detail.channel?.leadWakerId ? (
            <span>{t("form.lead")}</span>
          ) : null}
        </div>
      ))}
      <TeamFormDialog
        detail={detail}
        open={editing}
        onOpenChange={setEditing}
      />
    </>
  );
}
