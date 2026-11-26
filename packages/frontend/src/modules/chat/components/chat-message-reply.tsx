import { Reply, X } from "lucide-react";
import { createContext, useContext, useId } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { ComposerContext } from "@/shared/ui/composer";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import type {
  BotChatMessage,
  ChatReplyReference,
} from "../runtime/chat-projection";
import {
  isTerminalMessage,
  type ChatMessageSegment,
} from "./chat-message-nodes";

export const ChatMessageReplyActions = createContext<{
  select?: (replyTo: ChatReplyReference) => void;
  locate?: (messageId: string, keyboardTrigger?: HTMLButtonElement) => void;
}>({});

const replyPreviewLimit = 160;

function replyText(
  message: BotChatMessage,
  segments: readonly ChatMessageSegment[] | undefined,
  memberReport: boolean,
) {
  if (message.role === "user") {
    return !message.attachmentOnly &&
      !message.codeReferenceOnly &&
      (memberReport || !message.actor || message.actor === "user")
      ? message.content
      : undefined;
  }
  if (message.role !== "assistant" || !isTerminalMessage(message)) return;
  return segments
    ?.flatMap((segment) =>
      !("kind" in segment) &&
      segment.part.type === "text" &&
      segment.part.id !== `${message.id}:delivery` &&
      segment.part.text?.trim()
        ? [segment.part.text]
        : [],
    )
    .join("\n\n");
}

export function useSelectMessageReply(
  message: BotChatMessage,
  segments?: readonly ChatMessageSegment[],
  allowMemberReport = false,
) {
  const { select } = useContext(ChatMessageReplyActions);
  const { id, createdSequence, optimistic, question, inherited, sourceChatId } =
    message;
  const memberReport = Boolean(
    allowMemberReport &&
    message.actor === "team_member" &&
    sourceChatId &&
    message.sourceWakerId,
  );
  if (
    !select ||
    !id ||
    (createdSequence ?? 0) <= 0 ||
    optimistic ||
    question ||
    (sourceChatId && !inherited && !memberReport)
  )
    return;
  const text = replyText(message, segments, memberReport);
  const role = memberReport ? "assistant" : message.role;
  if (!text?.trim() || (role !== "assistant" && role !== "user")) return;
  return () => {
    const normalized = Array.from(text.trim());
    const preview =
      normalized.length > replyPreviewLimit
        ? `${normalized
            .slice(0, replyPreviewLimit - 1)
            .join("")
            .trimEnd()}…`
        : normalized.join("");
    select({
      messageId: id,
      role,
      preview,
    });
  };
}

export function MessageReplyAction({ onSelect }: { onSelect: () => void }) {
  const { t } = useTranslation("chat");
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t("reply")}
          onClick={onSelect}
        >
          <Reply />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t("reply")}</TooltipContent>
    </Tooltip>
  );
}

export function MessageReplyQuote({
  replyTo,
  authorName,
}: {
  replyTo: ChatReplyReference;
  authorName?: string;
}) {
  const { t } = useTranslation("chat");
  const { locate } = useContext(ChatMessageReplyActions);
  const authorId = useId();
  const author =
    authorName ?? (replyTo.role === "user" ? t("replyAuthorYou") : undefined);
  return (
    <Button
      type="button"
      variant="plain"
      size={null}
      className="text-user-message-content/75 hover:text-user-message-content active:text-user-message-content mb-1 flex w-fit max-w-full min-w-0 justify-start text-start"
      aria-label={t("locateReply", { preview: replyTo.preview })}
      aria-describedby={author ? authorId : undefined}
      title={replyTo.preview}
      onClick={(event) =>
        locate?.(
          replyTo.messageId,
          event.detail === 0 ? event.currentTarget : undefined,
        )
      }
      disabled={!locate}
    >
      <span className="type-ui border-user-message-content/35 block min-w-0 border-s-2 ps-2">
        {author ? (
          <span
            id={authorId}
            className="text-user-message-content block truncate font-semibold"
          >
            {author}
          </span>
        ) : null}
        <span className="line-clamp-3 font-normal wrap-anywhere whitespace-pre-line">
          {replyTo.preview}
        </span>
      </span>
    </Button>
  );
}

export function ComposerReplyContext({
  replyTo,
  onDismiss,
}: {
  replyTo: ChatReplyReference;
  onDismiss: () => void;
}) {
  const { t } = useTranslation("chat");
  const label = t("replyPreview", { preview: replyTo.preview });
  return (
    <ComposerContext
      data-chat-composer-reply
      className="text-content-secondary"
    >
      <Reply aria-hidden="true" className="size-icon-sm shrink-0" />
      <span
        className="min-w-0 flex-1 truncate"
        title={label}
        aria-live="polite"
      >
        {label}
      </span>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="shrink-0"
            aria-label={t("removeReply")}
            onClick={onDismiss}
          >
            <X />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t("removeReply")}</TooltipContent>
      </Tooltip>
    </ComposerContext>
  );
}
