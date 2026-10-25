import type { ReactNode } from "react";
import { Wrench } from "lucide-react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Card } from "@/shared/ui/card";
import { DetailSection } from "@/shared/ui/detail-section";
import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { connectorTransportOf } from "../model/connector-view";
import type { ConnectorRecord, ConnectorCompatibility } from "../model/types";

/** 独立连接器与插件组件共用信息布局；诊断操作由拥有连接器的入口提供。 */
export function ConnectorDetails({
  connector,
  compatibility = [],
  testing = false,
  testError,
  testAction,
  managed = false,
}: {
  connector: ConnectorRecord;
  compatibility?: ConnectorCompatibility[];
  testing?: boolean;
  testError?: unknown;
  testAction?: ReactNode;
  managed?: boolean;
}) {
  const { t } = useTranslation("connector");
  const transport = connectorTransportOf(connector.transport);
  const definitions =
    transport === "stdio"
      ? (connector.environment ?? [])
      : (connector.headers ?? []);
  const testedAt = connector?.testedAt
    ? new Intl.DateTimeFormat("zh-CN", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(connector.testedAt))
    : t("detail.neverTested");
  const testStatus = testing
    ? "testing"
    : (connector?.testStatus ?? "untested");
  const testStatusClassName =
    testStatus === "succeeded"
      ? "text-success"
      : testStatus === "failed"
        ? "text-destructive"
        : "text-content-secondary";
  const compatibilityGroups = [
    {
      supported: true,
      runtimes: compatibility.filter((runtime) => runtime.supported),
    },
    {
      supported: false,
      runtimes: compatibility.filter((runtime) => !runtime.supported),
    },
  ].filter((group) => group.runtimes.length > 0);

  return (
    <>
      <DetailSection title={t("detail.configuration")} surface="wash">
        <div className="@container/connector-config grid min-w-0 gap-5">
          <dl className="grid min-w-0 grid-cols-1 gap-x-8 gap-y-4 @[24rem]/connector-config:grid-cols-2 @[40rem]/connector-config:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)] @[24rem]/connector-config:[&>div:last-child]:col-span-2 @[40rem]/connector-config:[&>div:last-child]:col-span-1">
            <ReadOnlyField label={t("form.transport")}>
              {t(`transport.${transport}`)}
            </ReadOnlyField>
            <ReadOnlyField label={t("detail.timeout")}>
              {connector.timeoutMs
                ? `${connector.timeoutMs} ms`
                : managed
                  ? "—"
                  : "10000 ms"}
            </ReadOnlyField>
            {transport === "stdio" ? (
              <ReadOnlyField label={t("form.command")}>
                <code className="type-body">{connector.command || "—"}</code>
              </ReadOnlyField>
            ) : (
              <ReadOnlyField label={t("form.url")}>
                {connector.url || "—"}
              </ReadOnlyField>
            )}
          </dl>
          <dl className="grid min-w-0 gap-5">
            {transport === "stdio" ? (
              <ReadOnlyField label={t("form.arguments")}>
                <code className="type-body">
                  {(connector.arguments ?? []).join(" ") || "—"}
                </code>
              </ReadOnlyField>
            ) : null}
            <ReadOnlyField
              label={
                transport === "stdio"
                  ? t("form.environment")
                  : t("form.headers")
              }
            >
              {definitions.length ? (
                <ul className="grid min-w-0 gap-3">
                  {definitions.map((definition) => (
                    <li
                      key={definition.id ?? definition.name}
                      className="bg-muted/60 min-h-row-default grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-md px-3 py-2"
                    >
                      <code className="min-w-0 [overflow-wrap:anywhere]">
                        {definition.name}
                      </code>
                      <span className="text-content-secondary whitespace-nowrap">
                        {managed
                          ? t("plugin:connectorManaged")
                          : definition.configured
                            ? t("detail.configured")
                            : t("detail.notConfigured")}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <span className="text-content-secondary">
                  {t(managed ? "plugin:connectorManaged" : "form.noSecrets")}
                </span>
              )}
            </ReadOnlyField>
          </dl>
        </div>
      </DetailSection>

      <DetailSection
        title={t("detail.connectionAndCompatibility")}
        actions={testAction}
      >
        <div className="grid min-w-0 gap-6">
          <Card asChild variant="section">
            <section
              aria-label={t("detail.testTitle")}
              className="grid min-w-0 gap-4"
            >
              <div className="@container/connector-test min-w-0">
                <dl className="grid min-w-0 grid-cols-1 gap-x-8 gap-y-4 @[24rem]/connector-test:grid-cols-2 @[36rem]/connector-test:grid-cols-3">
                  <ReadOnlyField label={t("detail.testStatus")}>
                    <span className={testStatusClassName} aria-live="polite">
                      {t(`testStatus.${testStatus}`)}
                    </span>
                  </ReadOnlyField>
                  <ReadOnlyField label={t("detail.testedAt")}>
                    {testedAt}
                  </ReadOnlyField>
                  <ReadOnlyField label={t("detail.testDuration")}>
                    {connector.testedAt
                      ? `${connector.testDurationMs ?? 0} ms`
                      : "—"}
                  </ReadOnlyField>
                </dl>
              </div>
              {connector.testError || Boolean(testError) ? (
                <p
                  role="alert"
                  className="text-destructive type-body min-w-0 [overflow-wrap:anywhere]"
                >
                  {connector.testError ||
                    localApiErrorMessage(testError, t("detail.testFailed"))}
                </p>
              ) : null}
            </section>
          </Card>

          <Card asChild variant="section">
            <section aria-label={t("detail.compatibility")}>
              {compatibility.length ? (
                <div className="grid min-w-0 gap-4">
                  {compatibilityGroups.map((group) => (
                    <div
                      key={group.supported ? "supported" : "unsupported"}
                      className="grid min-w-0 gap-2"
                    >
                      <h3 className="text-content-secondary type-ui font-medium">
                        {group.supported
                          ? t("compatibility.supported")
                          : t("compatibility.unsupported")}
                      </h3>
                      <ul className="flex min-w-0 flex-wrap gap-x-6 gap-y-3">
                        {group.runtimes.map((runtime) => (
                          <li
                            key={runtime.runtimeId}
                            className="type-body inline-flex min-w-0 flex-wrap items-center gap-2"
                          >
                            <RuntimeCliIcon
                              runtimeId={runtime.runtimeId}
                              size={14}
                            />
                            <span className="font-medium [overflow-wrap:anywhere]">
                              {runtimeDisplayName({
                                id: runtime.runtimeId,
                              }) || "Runtime"}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-content-secondary type-body">
                  {t("detail.noCompatibility")}
                </p>
              )}
            </section>
          </Card>
        </div>
      </DetailSection>

      <DetailSection
        title={t("detail.tools")}
        actions={
          connector.testedAt && connector.testToolCount !== undefined ? (
            <span className="text-content-secondary type-ui">
              {t("card.toolCount", { count: connector.testToolCount })}
            </span>
          ) : undefined
        }
      >
        {connector.testTools?.length ? (
          <ul className="grid min-w-0 gap-2">
            {connector.testTools.map((tool) => (
              <li
                key={tool.name}
                className="bg-muted/60 grid min-w-0 grid-cols-[20px_minmax(0,1fr)] items-center gap-3 rounded-md px-3 py-2"
              >
                <Wrench
                  className="text-content-secondary size-icon-md"
                  aria-hidden="true"
                />
                <div className="min-w-0">
                  <h3
                    className="type-label truncate font-medium"
                    title={tool.name}
                  >
                    {tool.name}
                  </h3>
                  <p
                    className="text-content-secondary type-body mt-1 truncate"
                    title={tool.description || t("detail.noToolDescription")}
                  >
                    {tool.description || t("detail.noToolDescription")}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <div className="grid min-w-0 gap-1">
            <p className="type-body font-medium">{t("detail.noTools")}</p>
            <p className="text-content-secondary type-body">
              {t(
                managed
                  ? "plugin:connectorToolsEmpty"
                  : "detail.noToolsDescription",
              )}
            </p>
          </div>
        )}
      </DetailSection>
    </>
  );
}
