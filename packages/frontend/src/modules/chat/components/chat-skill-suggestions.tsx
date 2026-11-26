import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { useId, useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";

import {
  listChatSkillSuggestionsOptions,
  listChatSkillSuggestionsQueryKey,
  resolveChatSkillSuggestionMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListChatSkillSuggestionsResponse } from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { resourceLibraryDetailPath } from "@/shared/lib/resource-navigation";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";
import type { ChatTimelineEvent } from "./chat-thread";
import { ChatTimelineMarker } from "./chat-timeline-marker";

type Suggestion = NonNullable<
  ListChatSkillSuggestionsResponse["items"]
>[number];
type Partner = { id?: string; name?: string };

const skillChangeLabels: Record<string, string> = {
  create: "skillLearning.created",
  revise: "skillLearning.updated",
  uninstall: "skillLearning.uninstalled",
  delete: "skillLearning.deleted",
};

const connectorChangeLabels: Record<string, string> = {
  create: "connectorChanges.created",
  update: "connectorChanges.updated",
  detach: "connectorChanges.detached",
  delete: "connectorChanges.deleted",
};

function suggestionsOptions(
  chatId: string,
  fromSequence?: number,
  fromTime?: number,
) {
  return listChatSkillSuggestionsOptions(
    withLocalErrorSurface({
      path: { chatId },
      query: { fromSequence, fromTime },
    }),
  );
}

export function useChatSkillTimeline({
  chatId,
  wakerId,
  active,
  windowReady,
  fromSequence,
  fromTime,
  partners,
  skillCategories,
  connectorCategories,
}: {
  chatId: string;
  wakerId: string;
  active: boolean;
  windowReady: boolean;
  fromSequence?: number;
  fromTime?: number;
  partners: readonly Partner[];
  skillCategories: ReadonlyMap<string, string>;
  connectorCategories: ReadonlyMap<string, string>;
}) {
  const { t } = useTranslation("chat");
  const path = { chatId };
  const queryKey = listChatSkillSuggestionsQueryKey({ path });
  const options = suggestionsOptions(chatId, fromSequence, fromTime);
  const enabled = Boolean(chatId && wakerId && windowReady);
  const suggestions = useQuery({
    ...options,
    enabled: active && enabled,
    placeholderData: (previous, query) => {
      const previousKey = query?.queryKey[0];
      if (
        previousKey?.path?.chatId === chatId &&
        (previousKey.query?.fromTime ?? 0) >= (fromTime ?? 0) &&
        (previousKey.query?.fromSequence ?? 0) >= (fromSequence ?? 0)
      ) {
        return previous;
      }
    },
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [queryKey],
    eventTypes: ["partner.review.suggestions.changed"],
    eventFilter: ({ payload }) =>
      Boolean(
        payload &&
        typeof payload === "object" &&
        "chatId" in payload &&
        payload.chatId === chatId,
      ),
    active,
    enabled,
  });
  const items = suggestions.data?.items;
  const changes = suggestions.data?.changes;
  const events = useMemo<ChatTimelineEvent[]>(() => {
    if (!chatId || !wakerId) return [];
    const deletedSkillIDs = new Set<string>();
    const deletedConnectorIDs = new Set<string>();
    for (const change of changes ?? []) {
      if (change.action !== "delete") continue;
      if (change.skillId) deletedSkillIDs.add(change.skillId);
      if (change.connectorId) deletedConnectorIDs.add(change.connectorId);
    }
    const learned = (items ?? []).flatMap((item) => {
      if (!item.id || !item.occurredAt || item.status === "ignored") return [];
      return [
        {
          id: `skill-learning:${item.id}`,
          createdAt: item.occurredAt,
          content:
            item.status === "pending" || item.status === "failed" ? (
              <ChatSkillSuggestionCard
                chatId={chatId}
                wakerId={wakerId}
                item={item}
                partners={partners}
                fromSequence={fromSequence}
                fromTime={fromTime}
              />
            ) : (
              <ChatSkillLearningMarker
                item={item}
                deleted={Boolean(
                  item.skillId && deletedSkillIDs.has(item.skillId),
                )}
                category={
                  item.skillId ? skillCategories.get(item.skillId) : undefined
                }
              />
            ),
        },
      ];
    });
    const managed = (changes ?? []).flatMap((change): ChatTimelineEvent[] => {
      const {
        action,
        name,
        occurredAt,
        sequence,
        skillId,
        connectorId,
        ownerWakerId,
      } = change;
      if (!occurredAt || !sequence || !name || !action) return [];
      const label = (connectorId ? connectorChangeLabels : skillChangeLabels)[
        action
      ];
      if (!label) return [];
      let href: string | undefined;
      if (
        connectorId &&
        !deletedConnectorIDs.has(connectorId) &&
        (action === "create" || action === "update")
      ) {
        href = resourceLibraryDetailPath(
          "connectors",
          connectorId,
          ownerWakerId,
        );
      } else if (
        skillId &&
        !deletedSkillIDs.has(skillId) &&
        (action === "create" || action === "revise")
      ) {
        href = resourceLibraryDetailPath("skills", skillId);
      }
      const category = connectorId
        ? connectorCategories.get(connectorId)
        : skillId
          ? skillCategories.get(skillId)
          : undefined;
      return [
        {
          id: `managed-resource:${sequence}`,
          createdAt: occurredAt,
          sequence,
          content: (
            <PublishedResourceMarker
              label={t(label)}
              name={name}
              href={href}
              icon={<ResourceCategoryIcon category={category} size="inline" />}
            />
          ),
        },
      ];
    });
    return [...learned, ...managed];
  }, [
    chatId,
    wakerId,
    items,
    changes,
    fromSequence,
    fromTime,
    partners,
    skillCategories,
    connectorCategories,
    t,
  ]);
  const loadError =
    active &&
    enabled &&
    (suggestions.isError || suggestions.data?.suggestionsUnavailable) ? (
      <div
        role="alert"
        className="type-ui text-content-secondary mt-4 flex flex-wrap items-center gap-2"
      >
        {localApiErrorMessage(suggestions.error, t("skillLearning.loadError"))}
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void suggestions.refetch()}
        >
          {t("common:retry")}
        </Button>
      </div>
    ) : undefined;
  return { events, loadError };
}

function ChatSkillLearningMarker({
  item,
  category,
  deleted,
}: {
  item: Suggestion;
  category?: string;
  deleted: boolean;
}) {
  const { t } = useTranslation("chat");
  if (item.status === "queued" || item.status === "running") {
    return (
      <ChatTimelineMarker>
        <span role="status">{t("skillLearning.saving")}</span>
      </ChatTimelineMarker>
    );
  }
  if (item.status !== "saved") return null;
  return (
    <PublishedResourceMarker
      label={t(
        item.action === "update"
          ? "skillLearning.updated"
          : "skillLearning.saved",
      )}
      name={item.name ?? ""}
      href={
        item.skillId && !deleted
          ? resourceLibraryDetailPath("skills", item.skillId)
          : undefined
      }
      icon={<ResourceCategoryIcon category={category} size="inline" />}
    />
  );
}

function PublishedResourceMarker({
  label,
  name,
  href,
  icon,
}: {
  label: string;
  name: string;
  href?: string;
  icon: ReactNode;
}) {
  return (
    <ChatTimelineMarker>
      <span>{label}</span>
      {href ? (
        <Button
          asChild
          size="sm"
          variant="ghost"
          className="max-w-full min-w-0"
        >
          <Link to={href}>
            {icon}
            <span className="truncate">{name}</span>
          </Link>
        </Button>
      ) : (
        <span className="min-w-0 break-words">{name}</span>
      )}
    </ChatTimelineMarker>
  );
}

function ChatSkillSuggestionCard({
  chatId,
  wakerId,
  item,
  partners,
  fromSequence,
  fromTime,
}: {
  chatId: string;
  wakerId: string;
  item: Suggestion;
  partners: readonly Partner[];
  fromSequence?: number;
  fromTime?: number;
}) {
  const { t, i18n } = useTranslation("chat");
  const heading = useId();
  const location = useLocation();
  const queryClient = useQueryClient();
  const path = { chatId };
  const { queryKey } = suggestionsOptions(chatId, fromSequence, fromTime);
  const historyKey = listChatSkillSuggestionsQueryKey({ path });
  const resolve = useMutation({
    ...withLocalErrorMutation(resolveChatSkillSuggestionMutation()),
    onSuccess: async (data) => {
      await queryClient.cancelQueries({ queryKey, exact: true });
      if (data?.id) {
        queryClient.setQueryData<ListChatSkillSuggestionsResponse>(
          queryKey,
          (current) =>
            current && {
              ...current,
              items: current.items?.map((currentItem) =>
                currentItem.id === data.id ? data : currentItem,
              ),
            },
        );
      }
      await queryClient.invalidateQueries({ queryKey: historyKey });
    },
    onError: () => queryClient.invalidateQueries({ queryKey: historyKey }),
  });
  const submit = (action: "save" | "ignore") => {
    if (!item.id || resolve.isPending) return;
    resolve.mutate({
      path: { chatId, suggestionId: item.id },
      body:
        action === "save" && item.approvalToken
          ? { action, approvalToken: item.approvalToken }
          : { action },
    });
  };
  const sharedPartners = (item.sharedPartnerIds ?? [])
    .filter((id) => id !== wakerId)
    .map((id) => partners.find((partner) => partner.id === id)?.name || id);
  const sharedLabels = new Intl.ListFormat(i18n.language || "zh-CN").format(
    sharedPartners,
  );
  const submittedApproval = resolve.variables?.body.approvalToken;
  const approvalChanged =
    item.status === "pending" &&
    resolve.data?.status === "pending" &&
    submittedApproval !== undefined &&
    submittedApproval !== item.approvalToken;
  const label = approvalChanged
    ? t("skillLearning.approvalChanged")
    : undefined;
  const failure =
    item.status === "failed"
      ? t(
          item.error === "versionConflict"
            ? "skillLearning.versionConflict"
            : item.error === "generationFailed"
              ? "skillLearning.generationFailed"
              : "skillLearning.failed",
        )
      : undefined;
  return (
    <Card
      asChild
      variant="section"
      className="bg-surface-subtle mt-4 grid gap-2"
    >
      <section aria-labelledby={heading}>
        <div className="flex min-w-0 items-start gap-2">
          <Sparkles
            aria-hidden
            className="size-icon-md text-content-secondary mt-0.5 shrink-0"
          />
          <h3
            id={heading}
            className="type-label min-w-0 font-medium break-words"
          >
            {item.name}
          </h3>
        </div>
        {item.lesson ? (
          <p className="type-body break-words whitespace-pre-wrap">
            {item.lesson}
          </p>
        ) : null}
        {item.sourceMessageIds?.length ? (
          <div className="flex flex-wrap gap-x-3 gap-y-1">
            {item.sourceMessageIds.map((messageId, index) => {
              const search = new URLSearchParams(location.search);
              search.set("message", messageId);
              return (
                <Link
                  key={messageId}
                  className="type-ui text-content-secondary underline"
                  to={{
                    pathname: location.pathname,
                    search: search.toString(),
                  }}
                >
                  {t("skillLearning.source", { position: index + 1 })}
                </Link>
              );
            })}
          </div>
        ) : null}
        {sharedLabels ? (
          <p className="type-body text-content-secondary break-words">
            {t("skillLearning.shared", { names: sharedLabels })}
          </p>
        ) : null}
        {label ? (
          <p className="type-body text-content-secondary" role="status">
            {label}
          </p>
        ) : null}
        {failure ? (
          <p role="alert" className="type-body text-destructive break-words">
            {failure}
          </p>
        ) : null}
        {resolve.isError ? (
          <p role="alert" className="type-body text-destructive break-words">
            {localApiErrorMessage(
              resolve.error,
              t("skillLearning.actionError"),
            )}
          </p>
        ) : null}
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            variant="secondary"
            disabled={resolve.isPending}
            onClick={() => submit("save")}
          >
            {t(
              item.status === "failed"
                ? "skillLearning.retry"
                : item.action === "update"
                  ? "skillLearning.update"
                  : "skillLearning.create",
            )}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={resolve.isPending}
            onClick={() => submit("ignore")}
          >
            {t("skillLearning.ignore")}
          </Button>
        </div>
      </section>
    </Card>
  );
}
