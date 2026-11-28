import { useTranslation } from "react-i18next";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { companionAttention } from "@/shared/ui/conversation-attention";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import type { MainConversationRequest } from "../model/main-conversation-message-group";

export type OpenMainConversationSource = (
  id: string,
  trigger: HTMLElement,
) => void;

export function MainConversationAvatar({
  name,
  src,
  workChatId,
  activity,
  openSource,
  openRecord,
  defaultConversation = false,
}: {
  name: string;
  src?: string;
  workChatId?: string;
  activity?: MainConversationRequest | "working";
  openSource?: OpenMainConversationSource;
  openRecord?: () => void;
  defaultConversation?: boolean;
}) {
  const { t } = useTranslation("chat");
  const request = activity === "working" ? undefined : activity;
  const attention = request
    ? companionAttention({ kind: "action", request })
    : activity && companionAttention({ kind: "working" });
  const avatar = (
    <Avatar
      name={name}
      src={src}
      size="sm"
      animated={Boolean(activity)}
      activity={activity && (activity === "working" ? "working" : "waiting")}
      attention={attention}
      interactive={false}
    />
  );
  const onOpen =
    workChatId && openSource
      ? (trigger: HTMLElement) => openSource(workChatId, trigger)
      : openRecord;
  if (!onOpen) return avatar;
  const label = t(
    defaultConversation ? "openDefaultConversation" : "openSource",
  );
  const description =
    activity &&
    t(
      activity === "input"
        ? "waitingElicitation"
        : activity === "approval"
          ? "waitingApproval"
          : "working",
    );
  const button = (
    <Button
      type="button"
      size="icon"
      variant="plain"
      className="-m-0.5"
      title={label}
      aria-label={label}
      aria-description={description}
      onClick={(event) => onOpen(event.currentTarget)}
    >
      {avatar}
    </Button>
  );
  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
