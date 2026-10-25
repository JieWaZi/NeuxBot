import { useQuery } from "@tanstack/react-query";
import { Plus, UsersRound } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";

import { listWakersOptions } from "../api/waker-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { WakerCard } from "../components/waker-card";
import { WakerFormDialog } from "../components/waker-form-dialog";
import { WakerListSkeleton } from "../components/waker-list-skeleton";
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

type WakerListPageProps = {
  renderTeamCollection?: () => ReactNode;
  renderTeamDialog?: (props: {
    onOpenChange: (open: boolean) => void;
    open: boolean;
  }) => ReactNode;
};

export function WakerListPage({
  renderTeamCollection,
  renderTeamDialog,
}: WakerListPageProps = {}) {
  const { t } = useTranslation("waker");
  const { t: tTeam } = useTranslation("team");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [teamDialogOpen, setTeamDialogOpen] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get("tab") === "teams" ? "teams" : "wakers";
  const keyword = searchParams.get("q")?.trim() ?? "";
  const query = useQuery({
    ...listWakersOptions(
      withLocalErrorSurface({
        query: {
          pageNum: 1,
          pageSize: 100,
          query: keyword || undefined,
        },
      }),
    ),
    enabled: activeTab === "wakers",
  });
  const wakers = query.data?.resources ?? [];
  const updateSearchParam = (key: string, value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value) next.delete(key);
      else next.set(key, value);
      return next;
    });
  };

  return (
    <main className="bg-surface-scene h-full min-w-0 overflow-hidden">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={activeTab === "teams" ? tTeam("title") : t("title")}
          description={
            activeTab === "teams" ? tTeam("description") : t("subtitle")
          }
          actions={
            <PageAction
              aria-label={activeTab === "teams" ? tTeam("create") : t("create")}
              onClick={() =>
                activeTab === "teams"
                  ? setTeamDialogOpen(true)
                  : setDialogOpen(true)
              }
            >
              <Plus />
              <PageActionLabel>
                {activeTab === "teams" ? tTeam("create") : t("create")}
              </PageActionLabel>
            </PageAction>
          }
        />

        {activeTab === "wakers" ? (
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
              count={t("count", { count: query.data?.totalSize ?? 0 })}
              installed={t("installed")}
            />
          </ResourcePageToolbar>
        ) : null}

        <section>
          {activeTab === "teams" ? (
            renderTeamCollection?.()
          ) : query.isPending ? (
            <div className="mt-6">
              <WakerListSkeleton />
            </div>
          ) : query.isError ? (
            <EmptyState
              className="mt-6"
              density="compact"
              title="暂时无法载入伙伴"
              actions={
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void query.refetch()}
                >
                  重新载入
                </Button>
              }
            />
          ) : wakers.length > 0 ? (
            <ResourceCardGrid className="mt-6">
              {wakers.map((waker) => (
                <WakerCard key={waker.id} waker={waker} />
              ))}
            </ResourceCardGrid>
          ) : (
            <EmptyState
              className="mt-6"
              density="compact"
              icon={UsersRound}
              title={keyword ? "没有找到匹配的伙伴" : "还没有伙伴"}
              actions={
                !keyword ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => setDialogOpen(true)}
                  >
                    新建第一位伙伴
                  </Button>
                ) : undefined
              }
            />
          )}
        </section>
      </ResourcePageSurface>
      <WakerFormDialog open={dialogOpen} onOpenChange={setDialogOpen} />
      {teamDialogOpen
        ? renderTeamDialog?.({
            open: true,
            onOpenChange: setTeamDialogOpen,
          })
        : null}
    </main>
  );
}
