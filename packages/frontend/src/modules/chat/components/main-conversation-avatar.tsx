import { useTranslation } from "react-i18next";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { companionAttention } from "@/shared/ui/conversation-attention";
import type { MainConversationAvatarActivity } from "../model/main-conversation-message-group";

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
}: {
  name: string;
  src?: string;
  workChatId: string;
  activity?: MainConversationAvatarActivity;
  openSource: OpenMainConversationSource;
}) {
  const { t } = useTranslation("chat");
  const request =
    activity === "input" || activity === "approval" ? activity : undefined;
  const attention = request
    ? companionAttention({ kind: "action", request })
    : activity === "working"
      ? companionAttention({ kind: "working" })
      : undefined;
  const avatar = (
    <Avatar
      name={name}
      src={src}
      size="sm"
      animated={Boolean(activity)}
      activity={activity && (request ? "waiting" : "working")}
      attention={attention}
      interactive={false}
    />
  );
  if (!workChatId) return avatar;
  const label = t("openSource", { name });
  const description =
    activity &&
    t(
      request === "input"
        ? "waitingElicitation"
        : request === "approval"
          ? "waitingApproval"
          : "working",
    );
  return (
    <Button
      type="button"
      size="icon"
      variant="plain"
      className="-m-0.5"
      title={label}
      aria-label={label}
      aria-description={description}
      onClick={(event) => openSource(workChatId, event.currentTarget)}
    >
      {avatar}
    </Button>
  );
}
