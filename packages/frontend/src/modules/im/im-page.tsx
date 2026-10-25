import { ChevronRight, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsContentShell } from "@/modules/settings";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { Skeleton } from "@/shared/ui/skeleton";
import { ConnectionDialog } from "./components/connection-dialog";
import { ConnectionSettingsRow } from "./components/connection-settings-row";
import { PlatformIcon } from "./components/platform-icon";
import { IMConnectionDetailDialog } from "./im-connection-detail-dialog";
import { useIMBindings, useIMConnections } from "./queries";
import type { IMPlatform } from "./types";

const supportedPlatforms = ["weixin", "feishu", "wecom"] as const;

export function IMPage() {
  const { t } = useTranslation("im");
  const query = useIMConnections();
  const bindings = useIMBindings();
  const [expandedPlatforms, setExpandedPlatforms] = useState<IMPlatform[]>([]);
  const [platform, setPlatform] = useState<IMPlatform>();
  const [detail, setDetail] = useState<{
    connectionId: string;
    trigger: HTMLElement;
  }>();

  return (
    <SettingsContentShell title={t("title")} description={t("description")}>
      {query.error ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p role="alert" className="type-body text-destructive">
            {localApiErrorMessage(query.error, t("operationFailed"))}
          </p>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void query.refetch()}
          >
            {t("reload")}
          </Button>
        </div>
      ) : null}
      <div className="flex flex-col gap-3">
        {supportedPlatforms.map((item) => {
          const connections =
            query.data?.resources?.filter(
              (connection) => connection.platform === item,
            ) || [];
          return (
            <Collapsible
              key={item}
              className="bg-surface-tertiary overflow-hidden rounded-lg"
              open={expandedPlatforms.includes(item)}
              onOpenChange={(open) =>
                setExpandedPlatforms((current) =>
                  open
                    ? [...current, item]
                    : current.filter((value) => value !== item),
                )
              }
            >
              <div className="min-h-row-rich flex flex-wrap items-center gap-x-2">
                <CollapsibleTrigger
                  aria-label={t("platformConnections", { platform: t(item) })}
                  className="group text-content-primary hover:bg-action-quiet-hover focus-visible:ring-ring flex min-w-0 flex-1 basis-48 items-center gap-2 self-stretch px-4 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
                >
                  <ChevronRight
                    aria-hidden="true"
                    className="size-icon-sm text-content-muted shrink-0 transition-transform group-data-[state=open]:rotate-90 motion-reduce:transition-none"
                  />
                  <PlatformIcon platform={item} compact />
                  <span className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
                    <span className="type-body font-medium break-words">
                      {t(item)}
                    </span>
                    <span className="type-ui text-content-secondary min-w-0 break-words">
                      {t("platformDescription." + item)}
                    </span>
                  </span>
                  <span className="type-ui text-content-secondary shrink-0 tabular-nums">
                    {query.isPending ? (
                      <Skeleton className="h-4 w-12" />
                    ) : query.data ? (
                      t("connectionCount", { count: connections.length })
                    ) : (
                      t("connections")
                    )}
                  </span>
                </CollapsibleTrigger>
                <div className="ml-auto flex max-w-full items-center px-3 py-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t("addPlatformBot", { platform: t(item) })}
                    onClick={() => setPlatform(item)}
                  >
                    <Plus />
                    {t("addBot")}
                  </Button>
                </div>
              </div>
              <CollapsibleContent>
                <div className="border-border-subtle divide-border-subtle divide-y border-t">
                  {query.isPending ? (
                    <div
                      role="status"
                      aria-busy="true"
                      aria-label={t("loading")}
                      className="grid gap-3 px-4 py-3"
                    >
                      <Skeleton className="h-4 w-1/3" />
                      <Skeleton className="h-4 w-2/3" />
                    </div>
                  ) : connections.length ? (
                    connections.map((connection) => (
                      <ConnectionSettingsRow
                        key={connection.id}
                        connection={connection}
                        bindings={
                          bindings.data?.filter(
                            (binding) => binding.connectionId === connection.id,
                          ) || []
                        }
                        onSaved={() => void query.refresh()}
                        onView={(trigger) => {
                          if (connection.id)
                            setDetail({ connectionId: connection.id, trigger });
                        }}
                      />
                    ))
                  ) : !query.error ? (
                    <p className="type-ui text-content-secondary px-4 py-3">
                      {t("empty.platformConnections", { platform: t(item) })}
                    </p>
                  ) : null}
                </div>
              </CollapsibleContent>
            </Collapsible>
          );
        })}
      </div>
      {bindings.error ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p role="alert" className="type-body text-destructive">
            {localApiErrorMessage(bindings.error, t("authorizationReadFailed"))}
          </p>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void bindings.refetch()}
          >
            {t("reload")}
          </Button>
        </div>
      ) : null}
      {detail ? (
        <IMConnectionDetailDialog
          key={detail.connectionId}
          connectionId={detail.connectionId}
          returnFocusTo={detail.trigger}
          onClose={() => setDetail(undefined)}
        />
      ) : null}
      {platform ? (
        <ConnectionDialog
          platform={platform}
          onClose={() => setPlatform(undefined)}
          onSaved={() => {
            setExpandedPlatforms((current) =>
              current.includes(platform) ? current : [...current, platform],
            );
            void query.refresh();
          }}
        />
      ) : null}
    </SettingsContentShell>
  );
}
