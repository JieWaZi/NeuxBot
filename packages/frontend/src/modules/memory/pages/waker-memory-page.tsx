import {
  PageAction,
  PageActionLabel,
  ResourcePageHeader,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  FolderClosed,
  LoaderCircle,
  Plus,
  Settings,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useSearchParams } from "react-router-dom";

import {
  deleteMemoryMutation,
  invalidateMemoryQueries,
  listMemoriesOptions,
} from "../api/memory-queries";
import { MemoryFormDialog } from "../components/memory-form-dialog";
import {
  MemoryList,
  MemoryListEmpty,
  MemoryListError,
} from "../components/memory-list";
import { MemorySettingsDialog } from "../components/memory-settings-dialog";
import { useDebouncedValue } from "../hooks/use-debounced-value";
import { useImeSafeDraft } from "../hooks/use-ime-safe-draft";
import type { MemoryResource, MemoryScope } from "../model/types";
import { listProjectsOptions } from "@/modules/project";
import { WakerDetailLayout } from "@/modules/waker";
import {
  localDateRangeBounds,
  normalizeDateInput,
} from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Skeleton } from "@/shared/ui/skeleton";

type ScopeTarget = {
  projectId: string;
  projectName: string;
  scope: MemoryScope;
};

type FormTarget = ScopeTarget & {
  memory: MemoryResource | null;
};

const PAGE_STEP = 20;

export function WakerMemoryPage() {
  const { t } = useTranslation("memory");
  const { wakerId = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const query = searchParams.get("search") ?? "";
  const debouncedQuery = useDebouncedValue(query);
  const from = normalizeDateInput(searchParams.get("from"));
  const to = normalizeDateInput(searchParams.get("to"));
  const range = localDateRangeBounds(from, to);
  const [visibleCount, setVisibleCount] = useState(PAGE_STEP);
  const [formTarget, setFormTarget] = useState<FormTarget | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MemoryResource | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  const projectsQuery = useQuery({
    ...listProjectsOptions({
      path: { wakerId },
      query: { pageNum: 1, pageSize: 100 },
    }),
    enabled: Boolean(wakerId),
  });
  const projects = projectsQuery.data?.resources ?? [];
  const memories = useQuery({
    ...listMemoriesOptions(
      withLocalErrorSurface({
        path: { wakerId },
        query: {
          pageNum: 1,
          pageSize: visibleCount,
          query: debouncedQuery || undefined,
          scope: "all",
          updatedAtEnd: range.end,
          updatedAtStart: range.start,
        },
      }),
    ),
    enabled: Boolean(wakerId) && range.valid,
    placeholderData: (previous) => previous,
  });
  const deleteMemory = useMutation({
    ...withLocalErrorMutation(deleteMemoryMutation()),
    onSuccess: () => invalidateMemoryQueries(queryClient),
  });

  const items = memories.data?.resources ?? [];
  const total = memories.data?.totalSize ?? 0;

  const scopeNameOf = (memory: MemoryResource) => {
    if (memory.scope !== "project") return t("scope.partner");
    const project = projects.find((item) => item.id === memory.scopeId);
    return project?.name || t("scope.unnamedProject");
  };

  const targetOf = (memory: MemoryResource): ScopeTarget =>
    memory.scope === "project"
      ? {
          projectId: memory.scopeId ?? "",
          projectName: scopeNameOf(memory),
          scope: "project",
        }
      : { projectId: "", projectName: t("scope.partner"), scope: "partner" };

  const updateFilters = (updates: Record<string, string>) => {
    setVisibleCount(PAGE_STEP);
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      for (const [key, value] of Object.entries(updates)) {
        if (value) next.set(key, value);
        else next.delete(key);
      }
      return next;
    });
  };
  const searchDraft = useImeSafeDraft(query, (value) =>
    updateFilters({ search: value }),
  );
  const hasReachedEnd = items.length > 0 && items.length >= total;
  const memoryListFooter =
    memories.isPending || memories.isError ? null : items.length < total ? (
      <div className="flex justify-center">
        <Button
          disabled={memories.isFetching}
          size="sm"
          variant="ghost"
          onClick={() => setVisibleCount((count) => count + PAGE_STEP)}
        >
          {memories.isFetching
            ? t("expanding")
            : t("expandMore", { count: PAGE_STEP })}
        </Button>
      </div>
    ) : hasReachedEnd ? (
      <p className="text-muted-foreground type-caption mt-4 text-center">
        {t("noMore")}
      </p>
    ) : null;

  return (
    <WakerDetailLayout active="memory">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <>
              <PageAction
                variant="plain"
                hitArea="comfortable"
                aria-label={t("settingsAction")}
                onClick={() => setSettingsOpen(true)}
              >
                <Settings />
              </PageAction>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <PageAction aria-label={t("addAction")}>
                    <Plus />
                    <PageActionLabel>{t("addAction")}</PageActionLabel>
                  </PageAction>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    onSelect={() =>
                      setFormTarget({
                        memory: null,
                        projectId: "",
                        projectName: t("scope.partner"),
                        scope: "partner",
                      })
                    }
                  >
                    <UserRound />
                    {t("addPartnerAction")}
                  </DropdownMenuItem>
                  {projects.map((project) => (
                    <DropdownMenuItem
                      key={project.id}
                      onSelect={() =>
                        setFormTarget({
                          memory: null,
                          projectId: project.id ?? "",
                          projectName:
                            project.name || t("scope.unnamedProject"),
                          scope: "project",
                        })
                      }
                    >
                      <FolderClosed />
                      {project.name || t("scope.unnamedProject")}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          }
        />

        <ResourcePageToolbar
          filters={
            <>
              <DateRangePicker
                from={from}
                labels={{
                  clear: t("range.clear"),
                  empty: t("range.allTime"),
                  help: t("range.help"),
                  title: t("range.title"),
                  trigger: t("range.trigger"),
                }}
                to={to}
                onChange={(nextFrom, nextTo) =>
                  updateFilters({ from: nextFrom, to: nextTo })
                }
              />
              <ResourcePageSearchField
                aria-label={t("search")}
                placeholder={t("search")}
                {...searchDraft}
              />
            </>
          }
        >
          <h2 className="type-body font-medium">{t("tabs.memories")}</h2>
          <span className="text-muted-foreground type-caption">
            {t("overview.count", { count: total })}
          </span>
        </ResourcePageToolbar>

        <div className="mt-6">
          {memories.isPending ? (
            <div className="grid gap-4 py-4">
              {Array.from({ length: 5 }, (_, index) => (
                <Skeleton className="h-row-rich rounded-md" key={index} />
              ))}
            </div>
          ) : memories.isError ? (
            <MemoryListError
              text={t("loadError")}
              onRetry={() => void memories.refetch()}
            />
          ) : items.length ? (
            <MemoryList
              deleting={deleteMemory.isPending}
              items={items}
              scopeName={scopeNameOf}
              onDelete={setDeleteTarget}
              onEdit={(memory) =>
                setFormTarget({ ...targetOf(memory), memory })
              }
            />
          ) : (
            <MemoryListEmpty
              text={query ? t("manager.noResults") : t("empty")}
            />
          )}

          {memoryListFooter}
        </div>
      </ResourcePageSurface>

      {formTarget ? (
        <MemoryFormDialog
          memory={formTarget.memory}
          open
          projectId={formTarget.projectId}
          scope={formTarget.scope}
          scopeName={formTarget.projectName}
          wakerId={wakerId}
          onOpenChange={(open) => {
            if (!open) setFormTarget(null);
          }}
        />
      ) : null}
      <MemorySettingsDialog
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
      />
      <Dialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <DialogContent
          description={t("delete.question")}
          size="sm"
          title={t("delete.title")}
        >
          <div className="px-6 pb-5">
            <p className="text-muted-foreground type-body">
              {t("delete.description")}
            </p>
            {deleteMemory.isError ? (
              <p role="alert" className="text-destructive type-body mt-3">
                {localApiErrorMessage(deleteMemory.error, t("delete.failed"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={deleteMemory.isPending} variant="secondary">
                {t("delete.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={deleteMemory.isPending || !deleteTarget?.id}
              variant="danger"
              onClick={() => {
                if (!deleteTarget?.id) return;
                const target = targetOf(deleteTarget);
                deleteMemory.mutate(
                  {
                    path: { memoryId: deleteTarget.id, wakerId },
                    query: {
                      projectId:
                        target.scope === "project"
                          ? target.projectId
                          : undefined,
                      scope: target.scope,
                    },
                  },
                  { onSuccess: () => setDeleteTarget(null) },
                );
              }}
            >
              {deleteMemory.isPending ? (
                <LoaderCircle className="animate-spin" />
              ) : null}
              {deleteMemory.isPending
                ? t("delete.pending")
                : t("delete.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </WakerDetailLayout>
  );
}
