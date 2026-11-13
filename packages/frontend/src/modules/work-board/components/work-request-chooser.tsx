import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import {
  workAttentionHref,
  type WorkAttentionTarget,
} from "../model/navigation";

export type WaitingConversation = {
  key: string;
  request: WorkAttentionTarget;
};

/** 同一个原会话只提供队首目标，聚合数量仍由 Core 提供。 */
export function waitingConversations(
  requests: readonly WorkAttentionTarget[] = [],
) {
  const conversations = new Map<string, WaitingConversation>();
  const byTime = (left: WorkAttentionTarget, right: WorkAttentionTarget) =>
    (left.createdAt ?? 0) - (right.createdAt ?? 0) ||
    (left.id ?? "").localeCompare(right.id ?? "");
  for (const request of requests) {
    const key =
      request.chatId && request.kind !== "review"
        ? `chat:${request.chatId}`
        : `${request.ownerKind}:${request.runId ?? request.ownerId}`;
    const current = conversations.get(key)?.request;
    const sequence = request.requestSequence;
    const currentSequence = current?.requestSequence;
    if (
      !current ||
      (sequence && currentSequence
        ? sequence - currentSequence
        : byTime(request, current)) < 0
    )
      conversations.set(key, { key, request });
  }
  return [...conversations.values()].sort((left, right) =>
    byTime(left.request, right.request),
  );
}

export function WorkRequestChooser({
  choices,
  onClose,
  onOpen,
}: {
  choices?: WaitingConversation[];
  onClose: () => void;
  onOpen: (href: string) => void;
}) {
  const { t } = useTranslation("common");
  return (
    <Dialog
      open={Boolean(choices?.length)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent title={t("partnerNavigation.chooseActionChat")}>
        <ul className="grid min-w-0 gap-2 px-6 pb-4">
          {choices?.map(({ key, request }) => (
            <li key={key} className="min-w-0">
              <Button
                variant="ghost"
                className="w-full justify-start"
                onClick={() => {
                  onClose();
                  onOpen(workAttentionHref(request));
                }}
              >
                <span className="truncate">
                  {request.actorName && request.ownerKind === "team_thread"
                    ? request.actorName
                    : request.workTitle ||
                      request.title ||
                      t("partnerNavigation.untitledChat")}
                </span>
                <span className="text-content-secondary type-caption ms-auto shrink-0">
                  {t(`partnerNavigation.request.${request.kind}`)}
                </span>
              </Button>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
