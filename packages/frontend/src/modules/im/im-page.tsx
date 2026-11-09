import { MessageCircle, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import {
  SettingsCard,
  SettingsContentShell,
  SettingsGroup,
  SettingsNavigationRow,
} from "@/modules/settings";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { PageHeader } from "@/shared/ui/page-header";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { Skeleton } from "@/shared/ui/skeleton";
import { ConnectionDialog } from "./components/connection-dialog";
import { ConnectionSettingsRow } from "./components/connection-settings-row";
import { PlatformIcon } from "./components/platform-icon";
import { IMConnectionDetailPage } from "./im-connection-detail-page";
import { useIMBindings, useIMConnections } from "./queries";
import type { IMPlatform } from "./types";

const supportedPlatforms = ["weixin", "feishu", "wecom"] as const;

export function IMPage() {
  const { t } = useTranslation("im");
  const query = useIMConnections();
  const bindings = useIMBindings();
  const [params, setParams] = useSearchParams();
  const requested = params.get("platform");
  const selectedPlatform =
    supportedPlatforms.find((item) => item === requested) ??
    query.data?.resources?.find((item) => item.id === params.get("connection"))
      ?.platform;
  const selected = supportedPlatforms.find((item) => item === selectedPlatform);
  useEffect(() => {
    if (!requested && selected && params.has("connection")) {
      const next = new URLSearchParams(params);
      next.set("platform", selected);
      setParams(next, { replace: true });
    }
  }, [params, requested, selected, setParams]);
  function openPlatform(value?: IMPlatform) {
    const next = new URLSearchParams(params);
    if (value) {
      next.set("platform", value);
      next.set("tab", "im");
    } else {
      for (const key of [
        "platform",
        "tab",
        "detail",
        "connection",
        "binding",
        "chat",
      ])
        next.delete(key);
    }
    setParams(next);
  }
  const [platform, setPlatform] = useState<IMPlatform>();

  if (params.has("connection"))
    return (
      <IMConnectionDetailPage
        connectionId={params.get("connection") || ""}
        initialBindingId={
          params.get("binding") || params.get("chat") || undefined
        }
        onBack={(parent) => {
          const next = new URLSearchParams(params);
          for (const key of ["connection", "binding", "chat"]) next.delete(key);
          if (parent) next.set("platform", parent);
          setParams(next);
        }}
      />
    );

  return (
    <SettingsContentShell showHeader={false}>
      {query.error ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p role="alert" className="type-body text-destructive">
            {localApiErrorMessage(query.error, t("operationFailed"))}
          </p>
          <Button
            size="settings"
            variant="settings-outline"
            onClick={() => void query.refetch()}
          >
            {t("reload")}
          </Button>
        </div>
      ) : null}
      {selected ? (
        <>
          <PageHeader
            appearance="compact"
            layout="detail"
            title={t(selected)}
            backAction={{
              label: t("backToIntegrations"),
              onClick: () => openPlatform(),
            }}
            visual={
              <div className="bg-resource-surface flex size-12 shrink-0 items-center justify-center rounded-lg">
                <PlatformIcon platform={selected} />
              </div>
            }
            description={t("platformDescription." + selected)}
          />
          <SettingsGroup
            title={t("connections")}
            trailing={
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="plain"
                      size="icon"
                      aria-label={t("addPlatformBot", {
                        platform: t(selected),
                      })}
                      onClick={() => setPlatform(selected)}
                    >
                      <Plus />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    {t("addPlatformBot", { platform: t(selected) })}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            }
          >
            <SettingsCard>
              {query.isPending ? (
                <div
                  role="status"
                  aria-busy="true"
                  aria-label={t("loading")}
                  className="grid gap-3 px-4 py-3"
                >
                  <Skeleton className="h-4 w-1/3" />
                  <Skeleton className="h-row-detail w-full" />
                </div>
              ) : query.data?.resources?.filter(
                  (connection) => connection.platform === selected,
                ).length ? (
                query.data.resources
                  .filter((connection) => connection.platform === selected)
                  .map((connection) => (
                    <ConnectionSettingsRow
                      key={connection.id}
                      connection={connection}
                      bindings={
                        bindings.data?.filter(
                          (binding) => binding.connectionId === connection.id,
                        ) || []
                      }
                      onView={() => {
                        const next = new URLSearchParams(params);
                        next.set("connection", connection.id || "");
                        next.set("platform", selected);
                        setParams(next);
                      }}
                    />
                  ))
              ) : !query.error ? (
                <p className="type-body text-content-secondary px-4 py-3">
                  {t("empty.platformConnections", { platform: t(selected) })}
                </p>
              ) : null}
            </SettingsCard>
          </SettingsGroup>
        </>
      ) : params.has("connection") ? (
        <>
          <PageHeader
            appearance="compact"
            layout="detail"
            title="IM"
            visual={
              <div className="bg-resource-surface flex size-12 shrink-0 items-center justify-center rounded-lg">
                <MessageCircle className="size-icon-xl" />
              </div>
            }
          />
          {query.isPending ? (
            <div
              role="status"
              aria-busy="true"
              aria-label={t("loading")}
              className="bg-resource-surface grid gap-3 rounded-lg px-4 py-3"
            >
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-row-detail w-full" />
            </div>
          ) : !query.error ? (
            <p className="type-body text-content-secondary">
              {t("connectionNotFound")}
            </p>
          ) : null}
        </>
      ) : (
        <div className="bg-resource-surface overflow-hidden rounded-lg">
          {supportedPlatforms.map((item) => (
            <SettingsNavigationRow
              key={item}
              title={t(item)}
              ariaLabel={t("platformConnections", { platform: t(item) })}
              description={t("platformDescription." + item)}
              visual={<PlatformIcon platform={item} />}
              onClick={() => openPlatform(item)}
            />
          ))}
        </div>
      )}
      {bindings.error ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p role="alert" className="type-body text-destructive">
            {localApiErrorMessage(bindings.error, t("authorizationReadFailed"))}
          </p>
          <Button
            size="settings"
            variant="settings-outline"
            onClick={() => void bindings.refetch()}
          >
            {t("reload")}
          </Button>
        </div>
      ) : null}
      {platform ? (
        <ConnectionDialog
          platform={platform}
          onClose={() => setPlatform(undefined)}
          onSaved={() => {
            void query.refresh();
          }}
        />
      ) : null}
    </SettingsContentShell>
  );
}
