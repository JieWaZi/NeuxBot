import { ConnectorDetails } from "../components/connector-details";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Cable, Pencil, Play, Terminal, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useLocation } from "react-router-dom";

import {
  getWakerConnectorOptions,
  invalidateVisibleConnectorQueries,
  setWakerConnectorEnabledMutation,
  testWakerConnectorMutation,
} from "../api/connector-queries";
import { ConnectorDeleteDialog } from "../components/connector-delete-dialog";
import { ConnectorFormDialog } from "../components/connector-form-dialog";
import { connectorTransportOf } from "../model/connector-view";
import { WakerDetailLayout } from "@/modules/waker";
import { Badge } from "@/shared/ui/badge";
import { PageHeader } from "@/shared/ui/page-header";
import { PageAction } from "@/shared/ui/resource-page";
import { QueryState } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { Switch } from "@/shared/ui/switch";

export function ConnectorDetailPage() {
  const { t } = useTranslation("connector");
  const { connectorId = "", wakerId = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const active = useRouteSceneActive();
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId
      ? `/wakers/${wakerId}/resources?category=connectors`
      : "/resources?category=connectors");
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const detailOptions = getWakerConnectorOptions(
    withLocalErrorSurface({ path: { connectorId, wakerId } }),
  );
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: active && Boolean(connectorId),
  });
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

  return (
    <WakerDetailLayout active="connectors">
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          backAction={{
            label: t("common:backToList"),
            onClick: () => navigate(back),
          }}
          title={
            <span className="inline-flex max-w-full flex-wrap items-center gap-2">
              <span className="min-w-0 [overflow-wrap:anywhere]">
                {connector?.name ?? t("title")}
              </span>
              {connector ? (
                <Badge variant="secondary">
                  {t(`scope.${builtIn ? "built_in" : "public"}`)}
                </Badge>
              ) : null}
            </span>
          }
          description={connector?.description || undefined}
          visual={
            <span className="bg-muted text-muted-foreground grid size-10 place-items-center rounded-md">
              {transport === "stdio" ? (
                <Terminal className="size-icon-md" strokeWidth={1.5} />
              ) : (
                <Cable className="size-icon-md" strokeWidth={1.5} />
              )}
            </span>
          }
          actions={
            <div className="grid max-w-full justify-items-end gap-2">
              <div className="flex max-w-full flex-wrap items-center gap-2">
                {wakerId && !systemManaged ? (
                  <Switch
                    checked={detailQuery.data?.enabled ?? false}
                    disabled={!connector || toggleConnector.isPending}
                    aria-label={t(
                      detailQuery.data?.enabled
                        ? "detail.disable"
                        : "detail.enable",
                      { name: connector?.name ?? t("title") },
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
                ) : null}
                {connector && wakerId ? (
                  <span className="text-content-secondary type-body">
                    {systemManaged
                      ? t("systemManaged")
                      : detailQuery.data?.enabled
                        ? t("enabled")
                        : t("disabled")}
                  </span>
                ) : null}
                {canEdit ? (
                  <PageAction
                    intent="edit"
                    disabled={!connector}
                    label={t("detail.edit")}
                    onClick={() => setEditOpen(true)}
                  >
                    <Pencil />
                  </PageAction>
                ) : null}
                {!builtIn ? (
                  <PageAction
                    intent="delete"
                    disabled={!connector}
                    label={t("detail.delete")}
                    onClick={() => setDeleteOpen(true)}
                  >
                    <Trash2 />
                  </PageAction>
                ) : null}
              </div>
              {toggleConnector.isError ? (
                <p role="alert" className="text-destructive type-body">
                  {localApiErrorMessage(
                    toggleConnector.error,
                    t("toggleFailed"),
                  )}
                </p>
              ) : null}
            </div>
          }
        />

        <main className="mt-6 grid min-w-0 gap-8">
          {!connector ? (
            <QueryState
              loadingLayout="detail"
              pending={detailQuery.isPending}
              error={detailQuery.error}
              emptyLabel={t("detail.notFound")}
              onRetry={() => void detailQuery.refetch()}
            />
          ) : (
            <>
              {detailQuery.isError ? (
                <QueryState
                  error={detailQuery.error}
                  onRetry={() => void detailQuery.refetch()}
                />
              ) : null}
              <ConnectorDetails
                connector={connector}
                compatibility={detailQuery.data?.compatibility}
                testing={testConnector.isPending}
                testError={testConnector.error}
                testAction={
                  <PageAction
                    disabled={testConnector.isPending}
                    intent="primary"
                    label={t(
                      testConnector.isPending
                        ? "detail.testing"
                        : "detail.test",
                    )}
                    onClick={() =>
                      testConnector.mutate({ path: { connectorId, wakerId } })
                    }
                  >
                    <Play />
                  </PageAction>
                }
              />
            </>
          )}
        </main>
      </ResourcePageSurface>

      {connector ? (
        <ConnectorFormDialog
          open={editOpen}
          connector={connector}
          wakerId={wakerId}
          onOpenChange={setEditOpen}
        />
      ) : null}
      {connector && !builtIn ? (
        <ConnectorDeleteDialog
          open={deleteOpen}
          affectedWakerCount={detailQuery.data?.affectedWakerCount ?? 0}
          connectorId={connectorId}
          name={connector.name ?? ""}
          wakerId={wakerId}
          onOpenChange={setDeleteOpen}
          onDeleted={() => navigate(back)}
        />
      ) : null}
    </WakerDetailLayout>
  );
}
