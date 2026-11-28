import { useQueryClient } from "@tanstack/react-query";
import type { Dispatch, SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { useSettingsNavigationGuard } from "@/modules/settings";
import { captureMainConversationOpenIntent } from "@/shared/api/chat-reading";
import type { ConversationDestination } from "@/shared/lib/main-conversation-navigation";
import { chatDraftHref } from "@/shared/lib/chat-navigation";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";
import { useAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import type { PartnerNavigationPreferences } from "./partner-navigation-preferences";
import { mainConversationHref } from "../scenes/main-conversation-route";

export function useSubjectChatNavigation(
  owner: ChatSceneOwner | undefined,
  updatePreferences: Dispatch<SetStateAction<PartnerNavigationPreferences>>,
  closeRightPanel: () => void,
) {
  const { t } = useTranslation(["chat", "common"]);
  const navigate = useNavigate();
  const client = useQueryClient();
  const guard = useSettingsNavigationGuard();
  const scenes = useAppSceneWorkspace();

  function openMain(
    target: ConversationDestination,
    reading?: { observedGeneration?: number },
  ) {
    return guard(() => {
      updatePreferences((current) => ({
        ...current,
        currentPartnerId:
          target.kind === "waker" ? target.id : current.currentPartnerId,
        currentRailKey: `${target.kind}:${target.id}`,
        entityMode: target.kind,
      }));
      closeRightPanel();
      navigate(mainConversationHref(target), {
        flushSync: true,
        state:
          reading && !target.workChatId && !target.recordMessageId
            ? captureMainConversationOpenIntent(
                client,
                { subjectType: target.kind, subjectId: target.id },
                reading,
              )
            : undefined,
      });
    });
  }

  function openDraft() {
    if (scenes.openNewChat?.(owner) || !owner) return;
    closeRightPanel();
    const href =
      owner.kind === "team"
        ? `/teams/${owner.id}?draftId=${crypto.randomUUID()}`
        : chatDraftHref(owner.id);
    navigate(href, { flushSync: true });
  }
  return {
    openMain,
    openDraft,
    draftLabel:
      owner?.kind === "team"
        ? t("chat:newTeamThread")
        : t("common:partnerNavigation.newChat"),
  };
}
