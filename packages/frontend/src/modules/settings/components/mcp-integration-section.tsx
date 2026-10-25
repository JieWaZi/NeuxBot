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
import { Card } from "@/shared/ui/card";
import { Switch } from "@/shared/ui/switch";
import { Skeleton } from "@/shared/ui/skeleton";
import { mcpClientConfig, mcpConnection } from "../lib/mcp-connection";
import { useIntegrationCopy } from "../use-integration-copy";
import { SettingsGroup } from "./settings-section-layout";

export function MCPIntegrationSection() {
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
      // 启停仅返回成功回执，等待同一服务查询确认实际设置；刷新失败由原查询就近恢复。
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
    <SettingsGroup
      title={<label htmlFor={id}>{t("mcp.title")}</label>}
      description={t("mcp.description")}
      trailing={
        <Switch
          id={id}
          aria-label={t("mcp.switchLabel")}
          aria-busy={save.isPending}
          checked={enabled === true}
          disabled={
            !known || apiEnabled !== true || service.isPending || save.isPending
          }
          hitArea="comfortable"
          onClick={() => save.mutate({ body: { enabled: !enabled } })}
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
            <Skeleton className="h-40 w-full" />
          </div>
        ) : (
          <div className="grid min-w-0 gap-4">
            {!known ? (
              <p role="status" className="type-body text-content-secondary">
                {t("mcp.statusUnknown")}
              </p>
            ) : apiEnabled !== true ? (
              <p className="type-body text-content-secondary">
                {t("mcp.requiresAPI")}
              </p>
            ) : !effective ? (
              <p className="type-body text-content-secondary">
                {t("mcp.disabled")}
              </p>
            ) : (
              <>
                {!service.error && service.data?.available !== true && (
                  <p role="status" className="type-body text-content-secondary">
                    {t(
                      service.data?.available === false
                        ? "mcp.statusUnavailable"
                        : "mcp.statusUnknown",
                    )}
                  </p>
                )}
                {config ? (
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
                          copied === config
                            ? "mcp.copiedConfig"
                            : "mcp.copyConfig",
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
                ) : (
                  <p className="type-body text-content-secondary">
                    {t("mcp.noURL")}
                  </p>
                )}
              </>
            )}
            {save.isPending && (
              <p role="status" className="type-ui text-content-secondary">
                {t(
                  save.variables?.body.enabled
                    ? "mcp.enabling"
                    : "mcp.disabling",
                )}
              </p>
            )}
            {save.error && !service.error && (
              <p role="alert" className="type-body text-destructive">
                {localApiErrorMessage(save.error, t("failed"))}
              </p>
            )}
            {copyFailed && (
              <p role="alert" className="type-body text-destructive">
                {t("mcp.copyFailed")}
              </p>
            )}
          </div>
        )}
      </Card>
    </SettingsGroup>
  );
}
