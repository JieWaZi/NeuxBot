import { useQuery } from "@tanstack/react-query";
import { Plug, Webhook } from "lucide-react";
import { lazy, Suspense, type ReactNode } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import { integrationServiceOptions } from "@/shared/api/integration-service";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { SettingsCard } from "@/modules/settings";
import { PageHeader } from "@/shared/ui/page-header";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatusPill } from "@/shared/ui/status-pill";
import {
  integrationOverviewParams,
  integrationTabOf,
  type IntegrationTab,
} from "../settings-navigation";
import { useIntegrationCopy } from "../use-integration-copy";
const APIIntegrationSection = lazy(() =>
  import("./api-integration-section").then((module) => ({
    default: module.APIIntegrationSection,
  })),
);
const MCPIntegrationSection = lazy(() =>
  import("./mcp-integration-section").then((module) => ({
    default: module.MCPIntegrationSection,
  })),
);
import {
  SettingsContentShell,
  SettingsGroup,
  SettingsNavigationRow,
} from "./settings-section-layout";

/** 入站 API 与 MCP 仅复用服务查询，配置分别挂载在所属详情。 */
function PublishedService({
  tab,
  detail,
  onOpen,
  onBack,
  onOpenAPI,
}: {
  tab: "api" | "mcp";
  detail: boolean;
  onOpen: () => void;
  onBack: () => void;
  onOpenAPI: () => void;
}) {
  const t = useIntegrationCopy();
  const service = useQuery(integrationServiceOptions());
  const title = tab === "api" ? t("service") : t("mcp.service");
  const Icon = tab === "api" ? Webhook : Plug;
  const visual = (
    <span
      aria-hidden="true"
      className={cn(
        "text-resource-category-content grid shrink-0 place-items-center rounded-lg",
        detail ? "size-resource-detail-icon" : "size-resource-icon",
        tab === "api"
          ? "bg-resource-category-web"
          : "bg-resource-category-automation",
      )}
    >
      <Icon className="size-resource-logo" strokeWidth={1.5} />
    </span>
  );
  const enabled =
    tab === "api" ? service.data?.enabled : service.data?.mcpEnabled;
  const known =
    !service.error &&
    typeof enabled === "boolean" &&
    (tab === "api" || typeof service.data?.enabled === "boolean");
  const effective = enabled === true && service.data?.enabled === true;
  const available = service.data?.available;
  const label =
    !known || (effective && typeof available !== "boolean")
      ? t("mcp.statusUnknown")
      : !effective
        ? t("statusDisabled")
        : available
          ? t("statusAvailable")
          : t("statusUnavailable");
  return detail ? (
    <SettingsContentShell showHeader={false}>
      <PageHeader
        appearance="compact"
        layout="detail"
        title={title}
        backAction={{ label: t("back"), onClick: onBack }}
        visual={visual}
        status={
          <StatusPill
            tone={
              !known
                ? "idle"
                : effective && available === true
                  ? "connected"
                  : effective
                    ? "attention"
                    : "unavailable"
            }
          >
            {label}
          </StatusPill>
        }
      />
      <Suspense
        fallback={
          <div role="status" aria-busy="true" aria-label={t("serviceStatus")}>
            <Skeleton className="h-40 w-full" />
          </div>
        }
      >
        {tab === "api" ? (
          <APIIntegrationSection />
        ) : (
          <MCPIntegrationSection onOpenAPI={onOpenAPI} />
        )}
      </Suspense>
    </SettingsContentShell>
  ) : (
    <SettingsCard>
      <SettingsNavigationRow
        ariaLabel={title}
        title={title}
        description={service.isPending ? undefined : label}
        trailing={
          service.isPending ? (
            <span
              role="status"
              aria-busy="true"
              aria-label={t(tab === "api" ? "serviceStatus" : "mcp.loading")}
            >
              <Skeleton className="h-4 w-20" />
            </span>
          ) : undefined
        }
        visual={visual}
        onClick={onOpen}
      />
      {service.error ? (
        <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
          <p role="alert" className="type-resource-body text-destructive">
            {localApiErrorMessage(service.error, t("readFailed"))}
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
      ) : null}
    </SettingsCard>
  );
}

export function IntegrationsSection({ imContent }: { imContent?: ReactNode }) {
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const tab = integrationTabOf(location.pathname, params);
  const serviceDetail = params.get("detail") === "service" && tab !== "im";
  const platform = params.get("platform");
  const platformKnown =
    platform && ["weixin", "feishu", "wecom"].includes(platform);
  const imDetail =
    tab === "im" && Boolean(platformKnown || params.get("connection"));
  function overview() {
    setParams(integrationOverviewParams(params));
  }
  function open(value: IntegrationTab) {
    const next = new URLSearchParams(params);
    for (const key of ["platform", "connection", "binding", "chat"])
      next.delete(key);
    next.set("tab", value);
    next.set("detail", "service");
    setParams(next);
  }
  return (
    <>
      <SettingsContentShell showHeader={false}>
        {serviceDetail ? (
          <PublishedService
            key={tab}
            tab={tab}
            detail
            onOpen={() => open(tab)}
            onBack={overview}
            onOpenAPI={() => open("api")}
          />
        ) : imDetail ? (
          imContent
        ) : (
          <>
            <SettingsGroup title="IM">{imContent}</SettingsGroup>
            <SettingsGroup title="API">
              <PublishedService
                tab="api"
                detail={false}
                onOpen={() => open("api")}
                onBack={overview}
                onOpenAPI={() => open("api")}
              />
            </SettingsGroup>
            <SettingsGroup title="MCP">
              <PublishedService
                tab="mcp"
                detail={false}
                onOpen={() => open("mcp")}
                onBack={overview}
                onOpenAPI={() => open("api")}
              />
            </SettingsGroup>
          </>
        )}
      </SettingsContentShell>
    </>
  );
}
