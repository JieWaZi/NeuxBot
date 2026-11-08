import type { ReactNode } from "react";
import { Info, Wrench } from "lucide-react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { DetailSection } from "@/shared/ui/detail-section";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import {
  ResourceCatalogField,
  ResourceCatalogFields,
  ResourceCatalogRow,
  ResourceCatalogRows,
} from "@/shared/ui/resource-catalog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { connectorTransportOf } from "../model/connector-view";
import type { ConnectorRecord } from "../model/types";

/** 工具与完整配置分开阅读；测试诊断留在工具区的显式交互浮层中。 */
export function ConnectorDetails({
  connector,
  testing = false,
  testError,
  testAction,
  managed = false,
  source,
  version,
}: {
  connector: ConnectorRecord;
  testing?: boolean;
  testError?: unknown;
  testAction?: ReactNode;
  managed?: boolean;
  source?: ReactNode;
  version?: string;
}) {
  const { t, i18n } = useTranslation("connector");
  const transport = connectorTransportOf(connector.transport);
  const definitions =
    transport === "stdio"
      ? (connector.environment ?? [])
      : (connector.headers ?? []);
  const testedAt = connector.testedAt
    ? new Intl.DateTimeFormat(i18n.resolvedLanguage, {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(connector.testedAt))
    : t("detail.neverTested");
  const testStatus = testing ? "testing" : (connector.testStatus ?? "untested");
  const testStatusClassName =
    testStatus === "succeeded"
      ? "text-success"
      : testStatus === "failed"
        ? "text-destructive"
        : "text-resource-muted";
  const error = testError
    ? localApiErrorMessage(testError, t("detail.testFailed"))
    : connector.testError;

  return (
    <>
      <DetailSection
        appearance="catalog"
        title={t("detail.tools")}
        actions={
          <>
            {connector.testedAt && connector.testToolCount !== undefined && (
              <span className="type-resource-caption text-resource-muted">
                {t("card.toolCount", { count: connector.testToolCount })}
              </span>
            )}
            {(testing ||
              connector.testStatus === "succeeded" ||
              connector.testStatus === "failed") && (
              <span
                className={`type-resource-caption ${testStatusClassName}`}
                aria-live="polite"
              >
                {t(`testStatus.${testStatus}`)}
              </span>
            )}
            <Popover>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <PopoverTrigger asChild>
                      <Button
                        type="button"
                        size="catalog-icon"
                        variant="catalog-ghost"
                        aria-label={t("detail.testDetails")}
                      >
                        <Info aria-hidden="true" />
                      </Button>
                    </PopoverTrigger>
                  </TooltipTrigger>
                  <TooltipContent>{t("detail.testDetails")}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <PopoverContent
                width="default"
                side="bottom"
                align="end"
                aria-label={t("detail.testDetails")}
              >
                <section
                  aria-label={t("detail.testTitle")}
                  className="grid gap-3"
                >
                  <h3 className="type-resource-body text-resource-content font-medium">
                    {t("detail.testTitle")}
                  </h3>
                  <ResourceCatalogFields>
                    <ResourceCatalogField label={t("detail.testStatus")}>
                      <span className={testStatusClassName}>
                        {t(`testStatus.${testStatus}`)}
                      </span>
                    </ResourceCatalogField>
                    <ResourceCatalogField label={t("detail.testedAt")}>
                      {testedAt}
                    </ResourceCatalogField>
                    <ResourceCatalogField label={t("detail.testDuration")}>
                      {connector.testedAt
                        ? `${connector.testDurationMs ?? 0} ms`
                        : "—"}
                    </ResourceCatalogField>
                  </ResourceCatalogFields>
                </section>
              </PopoverContent>
            </Popover>
            {testAction}
          </>
        }
      >
        {error && (
          <p
            role="alert"
            className="type-resource-body text-destructive min-w-0 px-4 pt-3 [overflow-wrap:anywhere]"
          >
            {error}
          </p>
        )}
        {connector.testTools?.length ? (
          <ResourceCatalogRows>
            {connector.testTools.map((tool) => (
              <ResourceCatalogRow key={tool.name}>
                <span
                  className="size-resource-icon grid shrink-0 place-items-center"
                  aria-hidden="true"
                >
                  <Wrench
                    className="text-resource-secondary size-icon-lg"
                    strokeWidth={1.5}
                  />
                </span>
                <div className="min-w-0 flex-1">
                  <h3
                    className="type-resource-body text-resource-content truncate font-medium"
                    title={tool.name}
                  >
                    {tool.name}
                  </h3>
                  <p
                    className="type-resource-caption text-resource-muted mt-0.5 truncate"
                    title={tool.description || t("detail.noToolDescription")}
                  >
                    {tool.description || t("detail.noToolDescription")}
                  </p>
                </div>
              </ResourceCatalogRow>
            ))}
          </ResourceCatalogRows>
        ) : (
          <div className="grid min-w-0 gap-1 px-4 py-3">
            <p className="type-resource-body text-resource-content font-medium">
              {t("detail.noTools")}
            </p>
            <p className="type-resource-caption text-resource-muted">
              {t(
                managed
                  ? "plugin:connectorToolsEmpty"
                  : "detail.noToolsDescription",
              )}
            </p>
          </div>
        )}
      </DetailSection>
      <DetailSection appearance="catalog" title={t("detail.information")}>
        <div className="grid min-w-0 gap-3 px-4 py-3">
          {connector.description && (
            <p className="type-resource-body text-resource-content">
              {connector.description}
            </p>
          )}
          <ResourceCatalogFields>
            {Boolean(connector.categories?.length) && (
              <ResourceCatalogField label={t("common:resourceCategory.label")}>
                {connector.categories?.join("、")}
              </ResourceCatalogField>
            )}
            {source && (
              <ResourceCatalogField label={t("detail.source")}>
                {source}
              </ResourceCatalogField>
            )}
            {version && (
              <ResourceCatalogField label={t("detail.version")}>
                {version}
              </ResourceCatalogField>
            )}
            <ResourceCatalogField label={t("form.transport")}>
              {t(`transport.${transport}`)}
            </ResourceCatalogField>
            {transport === "stdio" ? (
              <>
                <ResourceCatalogField label={t("form.command")}>
                  <code>{connector.command || "—"}</code>
                </ResourceCatalogField>
                <ResourceCatalogField label={t("form.arguments")}>
                  <code>{(connector.arguments ?? []).join(" ") || "—"}</code>
                </ResourceCatalogField>
              </>
            ) : (
              <ResourceCatalogField label={t("form.url")}>
                {connector.url || "—"}
              </ResourceCatalogField>
            )}
            <ResourceCatalogField label={t("detail.timeout")}>
              {connector.timeoutMs
                ? t("detail.timeoutValue", {
                    seconds: connector.timeoutMs / 1000,
                  })
                : managed
                  ? "—"
                  : t("detail.timeoutValue", { seconds: 10 })}
            </ResourceCatalogField>
            <ResourceCatalogField
              label={t(
                transport === "stdio" ? "form.environment" : "form.headers",
              )}
            >
              {definitions.length ? (
                <ul className="grid min-w-0 gap-2">
                  {definitions.map((definition) => (
                    <li
                      key={definition.id ?? definition.name}
                      className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1"
                    >
                      <code className="min-w-0 [overflow-wrap:anywhere]">
                        {definition.name}
                      </code>
                      <span className="type-resource-caption text-resource-muted">
                        {t(
                          managed
                            ? "plugin:connectorManaged"
                            : definition.configured
                              ? "detail.configured"
                              : "detail.notConfigured",
                        )}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <span className="text-resource-muted">
                  {t(managed ? "plugin:connectorManaged" : "form.noSecrets")}
                </span>
              )}
            </ResourceCatalogField>
          </ResourceCatalogFields>
        </div>
      </DetailSection>
    </>
  );
}
