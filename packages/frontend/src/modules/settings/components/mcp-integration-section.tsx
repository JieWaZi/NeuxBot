import { useQuery } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { useId, useState } from "react";
import { integrationServiceOptions } from "@/shared/api/integration-service";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { PageAction } from "@/shared/ui/resource-page";
import { Skeleton } from "@/shared/ui/skeleton";
import { mcpClientConfig, mcpConnection } from "../lib/mcp-connection";
import { useIntegrationCopy } from "../use-integration-copy";
import { SettingsContentShell, SettingsGroup } from "./settings-section-layout";

export function MCPIntegrationSection({
  onOpenAPI,
}: {
  onOpenAPI?: () => void;
}) {
  const t = useIntegrationCopy();
  const id = useId();
  const service = useQuery(integrationServiceOptions());
  const connection = mcpConnection(service.data?.publicBaseUrl);
  const config = connection ? mcpClientConfig(connection.url) : undefined;
  const [copied, setCopied] = useState<string>();
  const [copyFailed, setCopyFailed] = useState(false);
  // 刷新失败或缺少明确字段时不沿用旧快照宣称服务仍在运行。
  const state = service.error
    ? "unknown"
    : service.data?.enabled === false
      ? "disabled"
      : service.data?.enabled === true && service.data.available === true
        ? "available"
        : service.data?.enabled === true && service.data.available === false
          ? "unavailable"
          : "unknown";
  async function copy(value: string) {
    setCopyFailed(false);
    try {
      await navigator.clipboard.writeText(value);
      setCopied(value);
    } catch {
      setCopied(undefined);
      setCopyFailed(true);
    }
  }
  return (
    <SettingsContentShell
      title={t("mcp.title")}
      description={t("mcp.description")}
      actions={
        <PageAction
          intent="secondary"
          label={t("mcp.manageAPI")}
          disabled={!onOpenAPI}
          onClick={onOpenAPI}
        />
      }
    >
      <Card variant="section">
        {service.isPending ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("mcp.loading")}
            className="grid gap-5"
          >
            <Skeleton className="h-row-default w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        ) : service.error && !service.data ? (
          <QueryState
            error={service.error}
            onRetry={() => void service.refetch()}
          />
        ) : (
          <div className="grid min-w-0 gap-5">
            <div
              role="status"
              aria-label={t("mcp.serviceStatus")}
              className="flex flex-wrap items-center justify-between gap-3"
            >
              <h2 className="type-label font-medium">{t("mcp.service")}</h2>
              <Badge
                variant={
                  state === "available"
                    ? "success"
                    : state === "unavailable"
                      ? "destructive"
                      : "secondary"
                }
              >
                {t(
                  state === "available"
                    ? "mcp.statusAvailable"
                    : state === "disabled"
                      ? "mcp.statusDisabled"
                      : state === "unavailable"
                        ? "mcp.statusUnavailable"
                        : "mcp.statusUnknown",
                )}
              </Badge>
            </div>
            <p className="type-ui text-content-secondary">
              {t("mcp.sharedService")}
            </p>
            <SettingsGroup title={t("mcp.url")} description={t("mcp.savedURL")}>
              {connection ? (
                <div className="flex min-w-0 flex-wrap items-center gap-3">
                  <code className="type-body min-w-0 flex-1 break-all select-text">
                    {connection.url}
                  </code>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void copy(connection.url)}
                  >
                    {copied === connection.url ? (
                      <Check aria-hidden="true" />
                    ) : (
                      <Copy aria-hidden="true" />
                    )}
                    {t(
                      copied === connection.url
                        ? "mcp.copiedURL"
                        : "mcp.copyURL",
                    )}
                  </Button>
                </div>
              ) : (
                <p className="type-body text-content-secondary">
                  {t("mcp.noURL")}
                </p>
              )}
            </SettingsGroup>
            {config && (
              <SettingsGroup
                title={t("mcp.config")}
                description={t("mcp.configHelp")}
                trailing={
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void copy(config)}
                  >
                    {copied === config ? (
                      <Check aria-hidden="true" />
                    ) : (
                      <Copy aria-hidden="true" />
                    )}
                    {t(
                      copied === config ? "mcp.copiedConfig" : "mcp.copyConfig",
                    )}
                  </Button>
                }
              >
                <pre
                  tabIndex={0}
                  aria-label={t("mcp.config")}
                  className="type-ui bg-surface-scene min-w-0 overflow-x-auto rounded-lg p-4 select-text"
                >
                  <code>{config}</code>
                </pre>
              </SettingsGroup>
            )}
            {copyFailed && (
              <p role="alert" className="type-body text-destructive">
                {t("mcp.copyFailed")}
              </p>
            )}
            {service.error && (
              <QueryRefreshError
                error={service.error}
                pending={service.isFetching}
                onRetry={() => void service.refetch()}
              />
            )}
          </div>
        )}
      </Card>
      <SettingsGroup
        title={t("mcp.permissions")}
        description={t("mcp.permissionsHelp")}
      >
        <ul className="type-body text-content-secondary grid list-disc gap-2 pl-5">
          <li>{t("mcp.retrieve")}</li>
          <li>{t("mcp.ingest")}</li>
          <li>{t("mcp.organize")}</li>
        </ul>
      </SettingsGroup>
      <section aria-labelledby={`${id}-reachability`} className="grid gap-3">
        <h2
          id={`${id}-reachability`}
          className="type-ui text-content-secondary font-medium"
        >
          {t("mcp.reachability")}
        </h2>
        {connection && (
          <p className="type-body text-content-secondary">
            {t(connection.loopback ? "mcp.loopback" : "mcp.remote")}
          </p>
        )}
        <p className="type-body text-content-secondary">{t("mcp.lifecycle")}</p>
      </section>
    </SettingsContentShell>
  );
}
