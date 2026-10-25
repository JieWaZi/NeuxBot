import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { ReactNode } from "react";
import { SettingsFieldGroup, SettingsFieldRow } from "@/modules/settings";
import { getProjectOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import {
  permissionLevelOf,
  permissionLevelTranslationKeys,
} from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import type { IMBinding } from "../types";

export function BindingOverview({ binding }: { binding: IMBinding }) {
  const { t } = useTranslation("im");
  const executors = useWorkExecutors();
  const team = executors.data?.teams.find(
    (item) => item.channel?.id === binding.channelId,
  );
  const owner = binding.channelId
    ? team?.channel?.leadWakerId
    : binding.wakerId;
  const projects = useQuery({
    ...getProjectOptions(
      withLocalErrorSurface({
        path: { wakerId: owner || "", projectId: binding.projectId || "" },
      }),
    ),
    enabled: Boolean(owner && binding.projectId),
  });
  const target = binding.channelId
    ? team?.channel?.name
    : executors.data?.wakers.find((item) => item.id === binding.wakerId)?.name;
  const level =
    permissionLevelOf(binding.options?.permissionLevel) || "default";
  const hasConfiguration = Boolean(binding.wakerId || binding.channelId);
  function loadedValue(
    pending: boolean,
    error: unknown,
    retry: () => void,
    value: ReactNode,
  ) {
    if (error)
      return (
        <span role="alert" className="text-destructive">
          {localApiErrorMessage(error, t("common:detailState.failed"))}
          <Button size="sm" variant="ghost" onClick={retry}>
            {t("reload")}
          </Button>
        </span>
      );
    return pending ? (
      <span role="status" aria-busy="true" aria-label={t("loading")}>
        <Skeleton className="h-4 w-24" />
      </span>
    ) : (
      value
    );
  }
  return (
    <div className="type-ui text-content-secondary grid min-w-0 gap-4">
      <SettingsFieldGroup>
        <SettingsFieldRow label={t("externalChatId")}>
          <span className="break-all select-text">
            {binding.externalChatId || binding.name || "—"}
          </span>
        </SettingsFieldRow>
        <SettingsFieldRow label={t("kind")}>
          {t(binding.kind === "group" ? "group" : "direct")}
        </SettingsFieldRow>
        <SettingsFieldRow label={t("executor")}>
          {hasConfiguration
            ? loadedValue(
                executors.isPending,
                executors.error,
                () => void executors.refetch(),
                <span className="[overflow-wrap:anywhere]">
                  {target || t("notConfigured")}
                </span>,
              )
            : t("notConfigured")}
        </SettingsFieldRow>
        {hasConfiguration ? (
          <>
            {binding.channelId ? (
              <SettingsFieldRow label={t("runtimeConfiguration")}>
                {t("teamDefaults")}
              </SettingsFieldRow>
            ) : (
              <>
                <SettingsFieldRow label={t("agent")}>
                  {runtimeDisplayName({ id: binding.options?.runtimeId }) ||
                    t("managed")}
                </SettingsFieldRow>
                <SettingsFieldRow label={t("model")}>
                  <span className="[overflow-wrap:anywhere]">
                    {binding.options?.model || t("managed")}
                  </span>
                </SettingsFieldRow>
                <SettingsFieldRow label={t("permission")}>
                  {t(`common:${permissionLevelTranslationKeys[level].label}`)}
                </SettingsFieldRow>
              </>
            )}
            <SettingsFieldRow label={t("scope")}>
              <span className="[overflow-wrap:anywhere]">
                {binding.workingDirectory ||
                  (binding.projectId
                    ? loadedValue(
                        owner ? projects.isPending : executors.isPending,
                        projects.error || (!owner ? executors.error : null),
                        () => {
                          void executors.refetch();
                          if (owner) void projects.refetch();
                        },
                        projects.data?.name || binding.projectId,
                      )
                    : t("managed"))}
              </span>
            </SettingsFieldRow>
            <SettingsFieldRow label={t("workflowDelivery")}>
              {t(binding.allowWorkflowDelivery ? "allowed" : "notAllowed")}
            </SettingsFieldRow>
          </>
        ) : null}
      </SettingsFieldGroup>
    </div>
  );
}
