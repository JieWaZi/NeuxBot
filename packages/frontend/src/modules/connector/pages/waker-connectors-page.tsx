import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import {
  ResourceBatchDeleteDialog,
  type ResourceDeleteTarget,
} from "@/shared/ui/resource-batch-delete-dialog";
import {
  PageAction,
  PageActionLabel,
  ResourceCardGrid,
  ResourcePageHeader,
  ResourcePageToolbar,
  ResourcePageLibraryLabel,
  ResourcePageSearchField,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import {
  deleteWakerConnectorMutation,
  invalidateVisibleConnectorQueries,
  listWakerConnectorsOptions,
  setWakerConnectorEnabledMutation,
} from "../api/connector-queries";
import { ConnectorCard } from "../components/connector-card";
import { ConnectorDeleteDialog } from "../components/connector-delete-dialog";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import { ConnectorFormDialog } from "../components/connector-form-dialog";
import { toConnectorSummary } from "../model/connector-view";
import type { ConnectorTransport } from "../model/types";
import { WakerDetailLayout } from "@/modules/waker";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";

type StatusFilter = "all" | "enabled" | "disabled";
type TransportFilter = "all" | ConnectorTransport;

export function WakerConnectorsPage() {
  const { t } = useTranslation("connector");
  const { wakerId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [batchTargets, setBatchTargets] = useState<ResourceDeleteTarget[]>([]);
  const deleteConnector = useMutation(
    withLocalErrorMutation(deleteWakerConnectorMutation()),
  );
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{
    affectedWakerCount: number;
    id: string;
    name: string;
  } | null>(null);
  const search = searchParams.get("search") ?? "";
  const status = statusFilterOf(searchParams.get("status"));
  const transport = transportFilterOf(searchParams.get("transport"));
  const connectorsQuery = useQuery({
    ...listWakerConnectorsOptions(withLocalErrorSurface({ path: { wakerId } })),
    enabled: Boolean(wakerId),
  });
  const toggleConnector = useMutation({
    ...withLocalErrorMutation(setWakerConnectorEnabledMutation()),
    onSuccess: () => invalidateVisibleConnectorQueries(queryClient),
  });
  const connectors = useMemo(
    () =>
      (connectorsQuery.data ?? []).flatMap(
        (item) => toConnectorSummary(item) ?? [],
      ),
    [connectorsQuery.data],
  );
  const visibleConnectors = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase();
    return connectors.filter((connector) => {
      const matchesSearch = normalizedSearch
        ? `${connector.name} ${connector.description}`
            .toLocaleLowerCase()
            .includes(normalizedSearch)
        : true;
      const matchesStatus =
        status === "all" ||
        (status === "enabled" ? connector.enabled : !connector.enabled);
      return (
        matchesSearch &&
        matchesStatus &&
        (transport === "all" || connector.transport === transport)
      );
    });
  }, [connectors, search, status, transport]);

  const selection = useResourceSelection(
    visibleConnectors.filter((item) => item.scope !== "built_in"),
    JSON.stringify([wakerId, search, status, transport]),
  );

  const updateSearchParam = (key: string, value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value || value === "all") next.delete(key);
      else next.set(key, value);
      return next;
    });
  };

  return (
    <WakerDetailLayout active="connectors">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <>
              <PageAction
                aria-label={t("add")}
                onClick={() => setCreateOpen(true)}
              >
                <Plus />
                <PageActionLabel>{t("add")}</PageActionLabel>
              </PageAction>
            </>
          }
        />

        <ResourcePageToolbar
          filters={
            <>
              <ResourceFilterSelect
                value={status}
                onValueChange={(value) => updateSearchParam("status", value)}
                ariaLabel={t("statusFilter")}
                options={[
                  { value: "all", label: t("allStatus") },
                  { value: "enabled", label: t("enabled") },
                  { value: "disabled", label: t("disabled") },
                ]}
              />
              <ResourceFilterSelect
                value={transport}
                onValueChange={(value) => updateSearchParam("transport", value)}
                ariaLabel={t("transportFilter")}
                options={(
                  ["all", "stdio", "streamable_http", "sse"] as const
                ).map((value) => ({
                  value,
                  label:
                    value === "all"
                      ? t("allTransports")
                      : t(`transport.${value}`),
                }))}
              />
              <ResourcePageSearchField
                value={search}
                onChange={(event) =>
                  updateSearchParam("search", event.target.value)
                }
                placeholder={t("search")}
                aria-label={t("search")}
              />
            </>
          }
        >
          <ResourcePageLibraryLabel
            installed={t("installed")}
            count={t("count", { count: visibleConnectors.length })}
          />
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        </ResourcePageToolbar>
        <ResourceSelectionActions
          count={selection.selected.length}
          onClear={selection.clear}
          onDelete={() =>
            setBatchTargets(
              selection.selected.map((item) => ({
                id: item.id,
                name: t("delete.affected", {
                  name: item.name,
                  count: item.affectedWakerCount,
                }),
              })),
            )
          }
        />

        {toggleConnector.isError ? (
          <p role="alert" className="text-destructive type-body mt-3">
            {localApiErrorMessage(toggleConnector.error, t("toggleFailed"))}
          </p>
        ) : null}

        {connectorsQuery.isPending ? (
          <ResourceLoadingState layout="cards" className="mt-6" />
        ) : connectorsQuery.isError ? (
          <EmptyState
            className="mt-6"
            density="compact"
            title={t("loadError")}
            actions={
              <Button
                size="sm"
                variant="secondary"
                onClick={() => void connectorsQuery.refetch()}
              >
                {t("common:retry")}
              </Button>
            }
          />
        ) : visibleConnectors.length > 0 ? (
          <ResourceCardGrid className="mt-6">
            {visibleConnectors.map((connector) => (
              <ConnectorCard
                key={connector.id}
                connector={connector}
                selected={selection.selectedIds.has(connector.id)}
                onSelectedChange={
                  connector.scope === "built_in"
                    ? undefined
                    : (value) => selection.setSelected(connector.id, value)
                }
                wakerId={wakerId}
                disabled={toggleConnector.isPending}
                onDelete={
                  connector.scope === "built_in"
                    ? undefined
                    : () =>
                        setDeleteTarget({
                          affectedWakerCount: connector.affectedWakerCount,
                          id: connector.id,
                          name: connector.name,
                        })
                }
                onEnabledChange={(enabled) =>
                  toggleConnector.mutate({
                    path: { connectorId: connector.id, wakerId },
                    body: { enabled },
                  })
                }
              />
            ))}
          </ResourceCardGrid>
        ) : (
          <EmptyState
            className="mt-6"
            density="compact"
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        )}
      </ResourcePageSurface>

      {batchTargets.length ? (
        <ResourceBatchDeleteDialog
          key={batchTargets.map((item) => item.id).join(":")}
          items={batchTargets}
          title={t("delete.batchTitle")}
          description={t("delete.description")}
          warning={t("delete.batchWarning")}
          onDelete={(connectorId) =>
            deleteConnector.mutateAsync({ path: { connectorId, wakerId } })
          }
          onDeleted={async (ids) => {
            selection.remove(ids);
            await invalidateVisibleConnectorQueries(queryClient);
          }}
          onOpenChange={(open) => {
            if (!open) setBatchTargets([]);
          }}
        />
      ) : null}
      <ConnectorFormDialog
        open={createOpen}
        wakerId={wakerId}
        onOpenChange={setCreateOpen}
        onSaved={(connectorId) => {
          if (connectorId)
            navigate(`/wakers/${wakerId}/connectors/${connectorId}`);
        }}
      />
      {deleteTarget ? (
        <ConnectorDeleteDialog
          open
          affectedWakerCount={deleteTarget.affectedWakerCount}
          connectorId={deleteTarget.id}
          name={deleteTarget.name}
          wakerId={wakerId}
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null);
          }}
        />
      ) : null}
    </WakerDetailLayout>
  );
}

function statusFilterOf(value: string | null): StatusFilter {
  return value === "enabled" || value === "disabled" ? value : "all";
}

function transportFilterOf(value: string | null): TransportFilter {
  return value === "stdio" || value === "streamable_http" || value === "sse"
    ? value
    : "all";
}
