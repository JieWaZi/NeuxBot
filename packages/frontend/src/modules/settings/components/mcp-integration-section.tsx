import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { useId, useState } from "react";
import {
  integrationMcpServiceMutation,
  integrationServiceOptions,
  integrationServiceQueryKey,
} from "@/shared/api/integration-service";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Switch } from "@/shared/ui/switch";
import { Skeleton } from "@/shared/ui/skeleton";
import { SettingsCard, SettingsGroup, SettingsRow } from "@/modules/settings";
import { mcpClientConfig, mcpConnection } from "../lib/mcp-connection";
import { useIntegrationCopy } from "../use-integration-copy";

export function MCPIntegrationSection({
  onOpenAPI,
}: {
  onOpenAPI?: () => void;
}) {
  const t = useIntegrationCopy();
  const id = useId();
  const client = useQueryClient();
  const service = useQuery(integrationServiceOptions());
  const connection = mcpConnection(service.data?.publicBaseUrl);
  const config = connection ? mcpClientConfig(connection.url) : undefined;
  const [copied, setCopied] = useState<string>();
  const [copyFailed, setCopyFailed] = useState(false);
  const enabled = service.data?.mcpEnabled;
  const apiEnabled = service.data?.enabled;
  const known =
    typeof enabled === "boolean" &&
    typeof apiEnabled === "boolean" &&
    !service.error;
  const effective = apiEnabled === true && enabled === true;
  const save = useMutation({
    ...integrationMcpServiceMutation(),
    onSuccess: async () => {
      setCopied(undefined);
      setCopyFailed(false);
      // 启停仅返回成功回执；读取失败保留旧配置，并由所属Card提供读取重试。
      await client.invalidateQueries({
        queryKey: integrationServiceQueryKey(),
      });
    },
  });
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
    <>
      <SettingsGroup title={t("mcp.serviceSettings")}>
        <SettingsCard>
          <SettingsRow
            grouped
            title={<label htmlFor={id}>{t("mcp.switchLabel")}</label>}
            description={t("mcp.description")}
          >
            <Switch
              id={id}
              aria-label={t("mcp.switchLabel")}
              aria-busy={save.isPending}
              checked={enabled === true}
              disabled={
                !known ||
                apiEnabled !== true ||
                service.isPending ||
                save.isPending
              }
              hitArea="comfortable"
              onClick={() => save.mutate({ body: { enabled: !enabled } })}
            />
          </SettingsRow>
          {service.isPending ? (
            <div
              role="status"
              aria-busy="true"
              aria-label={t("mcp.loading")}
              className="px-4 py-3"
            >
              <Skeleton className="h-5 w-full" />
            </div>
          ) : service.error ? (
            <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
              <p role="alert" className="type-resource-body text-destructive">
                {localApiErrorMessage(service.error, t("readFailed"))}
              </p>
              <Button
                type="button"
                size="settings"
                variant="settings-outline"
                disabled={service.isFetching || save.isPending}
                onClick={() => void service.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : !known ? (
            <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
              <p className="type-resource-body text-content-secondary">
                {t("mcp.configurationUnknown")}
              </p>
              <Button
                type="button"
                size="settings"
                variant="settings-outline"
                disabled={service.isFetching}
                onClick={() => void service.refetch()}
              >
                {t("retry")}
              </Button>
            </div>
          ) : apiEnabled !== true ? (
            <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
              <p className="type-resource-body text-content-secondary">
                {t("mcp.requiresAPI")}
              </p>
              {onOpenAPI ? (
                <Button
                  type="button"
                  size="settings"
                  variant="settings-outline"
                  onClick={onOpenAPI}
                >
                  {t("mcp.openAPI")}
                </Button>
              ) : null}
            </div>
          ) : null}
          {save.isPending ? (
            <p
              role="status"
              className="type-ui text-content-secondary px-4 py-3"
            >
              {t(
                save.variables?.body.enabled ? "mcp.enabling" : "mcp.disabling",
              )}
            </p>
          ) : null}
          {save.error ? (
            <p
              role="alert"
              className="type-resource-body text-destructive px-4 py-3"
            >
              {localApiErrorMessage(save.error, t("failed"))}
            </p>
          ) : null}
        </SettingsCard>
      </SettingsGroup>
      <SettingsGroup
        title={t("mcp.config")}
        trailing={
          effective && config ? (
            <Button
              type="button"
              size="settings"
              variant="settings-outline"
              disabled={!known || save.isPending}
              onClick={() => void copy(config)}
            >
              {copied === config ? (
                <Check aria-hidden="true" />
              ) : (
                <Copy aria-hidden="true" />
              )}
              {t(copied === config ? "mcp.copiedConfig" : "mcp.copyConfig")}
            </Button>
          ) : undefined
        }
      >
        <SettingsCard>
          {service.isPending ? (
            <div
              role="status"
              aria-busy="true"
              aria-label={t("mcp.config")}
              className="px-4 py-3"
            >
              <Skeleton className="h-40 w-full" />
            </div>
          ) : effective && config ? (
            <div className="grid min-w-0 gap-3 px-4 py-3">
              <p className="type-ui text-content-secondary">
                {t("mcp.configHelp")}
              </p>
              <pre
                tabIndex={0}
                aria-label={t("mcp.config")}
                className="type-ui min-w-0 overflow-x-auto select-text"
              >
                <code>{config}</code>
              </pre>
              {copyFailed ? (
                <p role="alert" className="type-resource-body text-destructive">
                  {t("mcp.copyFailed")}
                </p>
              ) : null}
            </div>
          ) : (
            <p className="type-resource-body text-content-secondary px-4 py-3">
              {t(
                !service.data || !known
                  ? "mcp.configurationUnavailable"
                  : !effective
                    ? "mcp.configurationDisabled"
                    : "mcp.noURL",
              )}
            </p>
          )}
        </SettingsCard>
      </SettingsGroup>
    </>
  );
}
