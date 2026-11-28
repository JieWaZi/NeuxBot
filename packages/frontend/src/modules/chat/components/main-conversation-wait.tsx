import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  getChannelOptions,
  getWakerOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ConversationSubject } from "../api/main-conversation";
import type { MainConversationWork } from "../model/main-conversation-message-group";
import { AssistantReplyWaitStatus, ChatReplyMessage } from "./chat-thread";
import {
  MainConversationAvatar,
  type OpenMainConversationSource,
} from "./main-conversation-avatar";

export function MainConversationWait({
  subject,
  work,
  openSource,
}: {
  subject: ConversationSubject;
  work?: MainConversationWork;
  openSource: OpenMainConversationSource;
}) {
  const { t } = useTranslation("chat");
  const isTeam = subject.subjectType === "team";
  const team = useQuery({
    ...getChannelOptions({ path: { channelId: subject.subjectId } }),
    enabled: isTeam && !work?.sourceWakerId,
  });
  const actor =
    work?.sourceWakerId ||
    (isTeam ? team.data?.channel?.leadWakerId : subject.subjectId) ||
    "";
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: actor } }),
    enabled: Boolean(actor),
  });
  const member = team.data?.members?.find(
    ({ waker }) => waker?.id === actor,
  )?.waker;
  return (
    <ChatReplyMessage
      message={{ role: "assistant" }}
      identity={
        <MainConversationAvatar
          name={author.data?.name || member?.name || t("wakerFallback")}
          src={author.data?.avatar || member?.avatar}
          workChatId={work?.workChatId}
          activity="working"
          openSource={openSource}
        />
      }
      status={<AssistantReplyWaitStatus showWaitingText />}
    >
      {null}
    </ChatReplyMessage>
  );
}
