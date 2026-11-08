import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Play } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useLocation } from "react-router-dom";
import { WakerDetailLayout } from "@/modules/waker";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  getWakerOptions,
  getWakerQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import { Button } from "@/shared/ui/button";
import { DetailSection } from "@/shared/ui/detail-section";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  ResourceCatalogRow,
  ResourceCatalogStatus,
} from "@/shared/ui/resource-catalog";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import { Switch } from "@/shared/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  getWakerConnectorOptions,
  invalidateVisibleConnectorQueries,
  setWakerConnectorEnabledMutation,
  testWakerConnectorMutation,
} from "../api/connector-queries";
import { ConnectorDeleteDialog } from "../components/connector-delete-dialog";
import { ConnectorDetailHeader } from "../components/connector-detail-header";
import { ConnectorDetails } from "../components/connector-details";
import { ConnectorFormDialog } from "../components/connector-form-dialog";
import { ConnectorPresentation } from "../components/connector-presentation";
import {
  connectorReferenceAvailable,
  connectorSelectableInComposer,
  connectorTransportOf,
} from "../model/connector-view";

export function ConnectorDetailPage() {
  const { t } = useTranslation("connector");
  const { connectorId = "", wakerId = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const active = useRouteSceneActive();
  const listPath = wakerId
    ? `/wakers/${encodeURIComponent(wakerId)}/resources?category=connectors`
    : "/resources?category=connectors";
  const candidate = (location.state as { resourceBack?: string } | null)
    ?.resourceBack;
  const directList = `/wakers/${encodeURIComponent(wakerId)}/connectors`;
  const back =
    wakerId &&
    candidate &&
    resourceListBack(candidate, directList) === candidate
      ? candidate
      : resourceListBack(candidate, listPath);
  const queryClient = useQueryClient();
  const identity = JSON.stringify([connectorId, wakerId]);
  const [editingIdentity, setEditingIdentity] = useState<string | null>(null);
  const [deletingIdentity, setDeletingIdentity] = useState<string | null>(null);
  const detailOptions = getWakerConnectorOptions(
    withLocalErrorSurface({ path: { connectorId, wakerId } }),
  );
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: active && Boolean(connectorId),
  });
  const partner = useQuery({
    ...getWakerOptions(withLocalErrorSurface({ path: { wakerId } })),
    queryKey: getWakerQueryKey({ path: { wakerId } }),
    enabled: active && Boolean(wakerId),
  });
  const partnerName =
    partner.data?.name || t("common:partnerNavigation.partner");
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [detailOptions.queryKey],
    active,
  });
  const refresh = () => invalidateVisibleConnectorQueries(queryClient);
  const toggleConnector = useMutation({
    ...withLocalErrorMutation(setWakerConnectorEnabledMutation()),
    onSuccess: refresh,
  });
  const testConnector = useMutation({
    ...withLocalErrorMutation(testWakerConnectorMutation()),
    onSuccess: refresh,
  });
  const currentTest =
    testConnector.variables?.path.connectorId === connectorId &&
    testConnector.variables.path.wakerId === wakerId;
  const currentToggle =
    toggleConnector.variables?.path.connectorId === connectorId &&
    toggleConnector.variables.path.wakerId === wakerId;
  const connector = detailQuery.data?.connector?.id
    ? detailQuery.data.connector
    : undefined;
  const builtIn = connector?.scope === "built_in";
  const systemManaged = connector?.activationPolicy === "workflow_context";
  const transport = connectorTransportOf(connector?.transport);
  const definitions =
    transport === "stdio"
      ? (connector?.environment ?? [])
      : (connector?.headers ?? []);
  const canEdit = !builtIn || definitions.length > 0;
  const removingInstallation = Boolean(wakerId && !connector?.ownerWakerId);

  return (
    <WakerDetailLayout
      active="connectors"
      breadcrumbItems={[
        { label: t("title"), href: back },
        { label: connector?.name || t("title") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <ConnectorDetailHeader
          title={connector?.name ?? t("title")}
          description={connector?.description}
          categories={connector?.categories}
          backAction={{
            label: t("common:backToList"),
            onClick: () => navigate(back),
          }}
          status={
            connector && wakerId ? (
              <ResourceCatalogStatus
                pill
                tone={detailQuery.data?.enabled ? "success" : "neutral"}
              >
                {t(
                  systemManaged
                    ? "systemManaged"
                    : detailQuery.data?.enabled
                      ? "enabled"
                      : "disabled",
                )}
              </ResourceCatalogStatus>
            ) : undefined
          }
          actions={
            canEdit && (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      size="catalog-icon"
                      variant="catalog-ghost"
                      disabled={!connector}
                      aria-label={t("detail.edit")}
                      onClick={() => setEditingIdentity(identity)}
                    >
                      <Pencil />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t("detail.edit")}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )
          }
        />
        <main className="mt-6 min-w-0 space-y-6">
          {!connector ? (
            <QueryState
              loadingLayout="catalog-detail"
              pending={detailQuery.isPending}
              error={detailQuery.error}
              emptyLabel={t("detail.notFound")}
              onRetry={() => void detailQuery.refetch()}
            />
          ) : (
            <>
              {detailQuery.isError && (
                <QueryRefreshError
                  error={detailQuery.error}
                  pending={detailQuery.isFetching}
                  onRetry={() => void detailQuery.refetch()}
                />
              )}
              <ConnectorPresentation
                key={identity}
                name={connector.name ?? ""}
                context={{ kind: "connector", connectorId }}
                wakerId={wakerId}
                usable={Boolean(
                  !detailQuery.isError &&
                  connectorReferenceAvailable(detailQuery.data, connectorId),
                )}
                systemManaged={
                  !connectorSelectableInComposer(connector.activationPolicy)
                }
              />
              {wakerId && !systemManaged && (
                <DetailSection
                  appearance="catalog"
                  title={t("detail.currentPartner")}
                >
                  <ResourceCatalogRow>
                    <ResourcePartnerAvatar
                      name={partnerName}
                      avatar={partner.data?.avatar}
                      size="md"
                    />
                    <div className="min-w-0 flex-1 space-y-1">
                      <h3
                        className="type-resource-body text-resource-content truncate font-medium"
                        title={partnerName}
                      >
                        {partnerName}
                      </h3>
                      <p className="type-resource-caption text-resource-muted">
                        {t(detailQuery.data?.enabled ? "enabled" : "disabled")}
                      </p>
                    </div>
                    <Switch
                      hitArea="comfortable"
                      checked={detailQuery.data?.enabled ?? false}
                      disabled={currentToggle && toggleConnector.isPending}
                      aria-label={t(
                        detailQuery.data?.enabled
                          ? "detail.disable"
                          : "detail.enable",
                        { name: connector.name },
                      )}
                      onClick={() =>
                        toggleConnector.mutate({
                          path: { connectorId, wakerId },
                          body: {
                            enabled: !(detailQuery.data?.enabled ?? false),
                          },
                        })
                      }
                    />
                    {removingInstallation && !builtIn && (
                      <Button
                        size="catalog"
                        variant="catalog-danger"
                        onClick={() => setDeletingIdentity(identity)}
                      >
                        {t("detail.removeConnector")}
                      </Button>
                    )}
                  </ResourceCatalogRow>
                  {currentToggle && toggleConnector.isError && (
                    <p
                      role="alert"
                      className="text-destructive type-resource-caption px-4 pb-3"
                    >
                      {localApiErrorMessage(
                        toggleConnector.error,
                        t("toggleFailed"),
                      )}
                    </p>
                  )}
                </DetailSection>
              )}
              <ConnectorDetails
                connector={connector}
                testing={currentTest && testConnector.isPending}
                testError={currentTest ? testConnector.error : null}
                testAction={
                  <Button
                    size="catalog"
                    variant="catalog-outline"
                    disabled={currentTest && testConnector.isPending}
                    onClick={() =>
                      testConnector.mutate({ path: { connectorId, wakerId } })
                    }
                  >
                    <Play />
                    {t(
                      currentTest && testConnector.isPending
                        ? "detail.testing"
                        : "detail.test",
                    )}
                  </Button>
                }
              />
              {!builtIn && !removingInstallation && (
                <DetailSection
                  appearance="catalog"
                  title={t("detail.dangerZone")}
                >
                  <ResourceCatalogRow className="flex-wrap">
                    <div className="min-w-0 flex-1 basis-60 space-y-1">
                      <h3 className="type-resource-body text-resource-content font-medium">
                        {t(
                          removingInstallation
                            ? "detail.removeConnector"
                            : "detail.delete",
                        )}
                      </h3>
                      <p className="type-resource-caption text-resource-muted">
                        {t(
                          removingInstallation
                            ? "detail.removeHelp"
                            : connector.ownerWakerId
                              ? "detail.privateDeleteHelp"
                              : "detail.deleteHelp",
                          { count: detailQuery.data?.affectedWakerCount ?? 0 },
                        )}
                      </p>
                    </div>
                    <Button
                      size="catalog"
                      variant="catalog-danger"
                      onClick={() => setDeletingIdentity(identity)}
                    >
                      {t(
                        removingInstallation
                          ? "detail.removeConnector"
                          : "detail.delete",
                      )}
                    </Button>
                  </ResourceCatalogRow>
                </DetailSection>
              )}
            </>
          )}
        </main>
      </ResourcePageSurface>
      {connector && (
        <ConnectorFormDialog
          key={identity}
          open={editingIdentity === identity}
          connector={connector}
          wakerId={wakerId}
          onOpenChange={(open) => setEditingIdentity(open ? identity : null)}
        />
      )}
      {connector && !builtIn && (
        <ConnectorDeleteDialog
          key={identity}
          open={deletingIdentity === identity}
          affectedWakerCount={detailQuery.data?.affectedWakerCount ?? 0}
          connectorId={connectorId}
          name={connector.name ?? ""}
          ownerWakerId={connector.ownerWakerId}
          wakerId={wakerId}
          onOpenChange={(open) => setDeletingIdentity(open ? identity : null)}
          onDeleted={() => navigate(back)}
        />
      )}
    </WakerDetailLayout>
  );
}
