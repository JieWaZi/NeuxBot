import { useQuery } from "@tanstack/react-query";
import { Network, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";

import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import {
  PageAction,
  PageActionLabel,
  ResourceCardGrid,
  ResourcePageHeader,
  ResourcePageLibraryLabel,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { Pagination } from "@/shared/ui/pagination";

import { listChannelsOptions } from "../api/channel-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { TeamCard } from "../components/team-card";
import { TeamFormDialog } from "../components/team-form-dialog";
import { TeamListSkeleton } from "../components/team-list-skeleton";

export function TeamListPage() {
  const { t } = useTranslation("team");
  const [creating, setCreating] = useState(false);

  return (
    <main className="bg-surface-scene h-full min-w-0 overflow-hidden">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <PageAction
              aria-label={t("create")}
              onClick={() => setCreating(true)}
            >
              <Plus />
              <PageActionLabel>{t("create")}</PageActionLabel>
            </PageAction>
          }
        />

        <TeamCollection />
      </ResourcePageSurface>
      {creating ? <TeamFormDialog open onOpenChange={setCreating} /> : null}
    </main>
  );
}

export function TeamCollection() {
  const { t } = useTranslation("team");
  const [searchParams, setSearchParams] = useSearchParams();
  const pageParam = Number(searchParams.get("teamPage"));
  const page = Number.isSafeInteger(pageParam) && pageParam > 0 ? pageParam : 1;
  const keyword = searchParams.get("q")?.trim() ?? "";
  const pageSize = 12;
  const channels = useQuery(
    listChannelsOptions(
      withLocalErrorSurface({
        query: {
          pageNum: page,
          pageSize,
          query: keyword || undefined,
        },
      }),
    ),
  );
  const totalItems = channels.data?.totalSize ?? 0;
  const updateSearchParam = (key: string, value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value) next.delete(key);
      else next.set(key, value);
      if (key === "q") next.delete("teamPage");
      return next;
    });
  };
  const setListLocation = (nextPage: number) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (nextPage > 1) next.set("teamPage", String(nextPage));
      else next.delete("teamPage");
      return next;
    });
  };

  return (
    <>
      <ResourcePageToolbar
        filters={
          <ResourcePageSearchField
            aria-label={t("search")}
            onChange={(event) => updateSearchParam("q", event.target.value)}
            placeholder={t("search")}
            value={keyword}
          />
        }
      >
        <ResourcePageLibraryLabel
          count={t("count", { count: totalItems })}
          installed={t("installed")}
        />
      </ResourcePageToolbar>

      {channels.isPending ? (
        <div className="mt-6">
          <TeamListSkeleton />
        </div>
      ) : channels.isError ? (
        <EmptyState
          className="mt-6"
          density="compact"
          title={t("loadError")}
          actions={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void channels.refetch()}
            >
              {t("retryLoad")}
            </Button>
          }
        />
      ) : channels.data?.resources?.length ? (
        <>
          <ResourceCardGrid className="mt-6">
            {channels.data.resources.map((detail) => (
              <TeamCard
                key={detail.channel?.id}
                detail={detail}
                onCollectionChanged={() => {
                  if ((channels.data?.resources?.length ?? 0) <= 1 && page > 1)
                    setListLocation(page - 1);
                }}
              />
            ))}
          </ResourceCardGrid>
          {totalItems > pageSize ? (
            <Pagination
              page={page}
              pageSize={pageSize}
              totalItems={totalItems}
              onPageChange={setListLocation}
              labels={{
                navigation: t("pagination.navigation"),
                previous: t("pagination.previous"),
                next: t("pagination.next"),
                page: (nextPage) => t("pagination.page", { page: nextPage }),
                pageSize: (size) => t("pagination.pageSize", { size }),
                pageSizeSelection: t("pagination.pageSizeSelection"),
                total: (count) => t("pagination.total", { count }),
              }}
            />
          ) : null}
        </>
      ) : (
        <EmptyState
          className="mt-6"
          density="compact"
          icon={Network}
          title={keyword ? t("emptySearch") : t("empty")}
          description={keyword ? undefined : t("emptyHint")}
        />
      )}
    </>
  );
}
