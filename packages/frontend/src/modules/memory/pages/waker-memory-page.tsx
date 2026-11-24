import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, Plus, Settings } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams, useSearchParams } from "react-router-dom";

import {
  deleteMemoryMutation,
  invalidatePartnerMemory,
  listMemoriesOptions,
  removeMemoryFromGroup,
  setMemoryCoreMutation,
  usePartnerMemoryEvents,
} from "../api/memory-queries";
import { MemoryFormDialog } from "../components/memory-form-dialog";
import { MemoryGroup } from "../components/memory-group";
import { useDebouncedValue } from "../hooks/use-debounced-value";
import { useImeSafeDraft } from "../hooks/use-ime-safe-draft";
import type { MemoryResource } from "../model/types";
import { WakerDetailLayout } from "@/modules/waker";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  localDateRangeBounds,
  normalizeDateInput,
} from "@/shared/lib/local-date";
import { partnerReviewSettingsHref } from "@/shared/lib/partner-review-settings";
import { Button } from "@/shared/ui/button";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import {
  PageAction,
  ResourcePageHeader,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";

const PAGE_SIZE = 20;

function readPage(value: string | null) {
  const page = Number(value);
  return Number.isSafeInteger(page) && page >= 1 ? page : 1;
}

export function WakerMemoryPage() {
  const { wakerId = "" } = useParams();
  return <PartnerMemory key={wakerId} wakerId={wakerId} />;
}

function PartnerMemory({ wakerId }: { wakerId: string }) {
  const { t } = useTranslation("memory");
  const [params, setParams] = useSearchParams();
  const queryClient = useQueryClient();
  const search = params.get("search") ?? "";
  const debouncedSearch = useDebouncedValue(search);
  const from = normalizeDateInput(params.get("from"));
  const to = normalizeDateInput(params.get("to"));
  const range = localDateRangeBounds(from, to);
  const corePage = readPage(params.get("corePage"));
  const regularPage = readPage(params.get("regularPage"));
  const queryEnabled = Boolean(wakerId) && range.valid;
  const sharedQuery = {
    pageSize: PAGE_SIZE,
    query: debouncedSearch || undefined,
    updatedAtEnd: range.end,
    updatedAtStart: range.start,
  };
  const core = useQuery({
    ...listMemoriesOptions(
      withLocalErrorSurface({
        path: { wakerId },
        query: { ...sharedQuery, core: true, pageNum: corePage },
      }),
    ),
    enabled: queryEnabled,
  });
  const regular = useQuery({
    ...listMemoriesOptions(
      withLocalErrorSurface({
        path: { wakerId },
        query: { ...sharedQuery, core: false, pageNum: regularPage },
      }),
    ),
    enabled: queryEnabled,
  });
  usePartnerMemoryEvents(wakerId);

  const [formTarget, setFormTarget] = useState<
    MemoryResource | null | undefined
  >(undefined);
  const [deleteTarget, setDeleteTarget] = useState<MemoryResource | null>(null);
  const [focusTarget, setFocusTarget] = useState<{
    core: boolean;
    id: string;
  }>();
  const coreGroup = useRef<HTMLElement>(null);
  const regularGroup = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    if (!focusTarget) return;
    const group = (focusTarget.core ? coreGroup : regularGroup).current;
    const action = Array.from(
      group?.querySelectorAll<HTMLButtonElement>("[data-memory-core-action]") ??
        [],
    ).find((button) => button.dataset.memoryCoreAction === focusTarget.id);
    (action ?? group?.querySelector("h2"))?.focus();
  }, [focusTarget]);

  const setCore = useMutation({
    ...withLocalErrorMutation(setMemoryCoreMutation()),
    onSuccess: async (_data, { body, path }) => {
      await removeMemoryFromGroup(
        queryClient,
        path.wakerId,
        path.memoryId,
        !body.core,
      );
      await invalidatePartnerMemory(queryClient, path.wakerId);
      setFocusTarget({ core: body.core, id: path.memoryId });
    },
  });
  const deleteMemory = useMutation({
    ...withLocalErrorMutation(deleteMemoryMutation()),
    onSuccess: (_data, { path }) =>
      invalidatePartnerMemory(queryClient, path.wakerId),
  });
  const toggleCore = (memory: MemoryResource) => {
    if (!memory.id || setCore.isPending) return;
    setCore.mutate({
      path: { wakerId, memoryId: memory.id },
      body: { core: !memory.core },
    });
  };

  const coreItems = core.data?.resources ?? [];
  const showCoreGroup =
    coreItems.length > 0 || (core.data?.totalSize ?? 0) > 0 || core.isError;
  const coreIds = new Set(coreItems.map(({ id }) => id));
  const regularItems = (regular.data?.resources ?? []).filter(
    ({ id }) => !coreIds.has(id),
  );
  const updateFilters = (updates: Record<string, string>) => {
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.delete("corePage");
      next.delete("regularPage");
      for (const [key, value] of Object.entries(updates)) {
        if (value) next.set(key, value);
        else next.delete(key);
      }
      return next;
    });
  };
  const searchDraft = useImeSafeDraft(search, (value) =>
    updateFilters({ search: value }),
  );
  const changePage = (key: "corePage" | "regularPage", page: number) => {
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (page > 1) next.set(key, String(page));
      else next.delete(key);
      return next;
    });
  };
  const groupActions = {
    pending: setCore.isPending,
    changingId: setCore.variables?.path.memoryId,
    error: setCore.isError ? setCore.error : undefined,
    deleting: deleteMemory.isPending,
    onToggleCore: toggleCore,
    onEdit: setFormTarget,
    onDelete: (memory: MemoryResource) => {
      deleteMemory.reset();
      setDeleteTarget(memory);
    },
  };
  const filtered = Boolean(search || from || to);

  return (
    <WakerDetailLayout active="memory">
      <ResourcePageSurface appearance="catalog">
        <ResourcePageHeader
          appearance="catalog"
          title={t("title")}
          description={undefined}
        />
        <ResourcePageToolbar
          appearance="catalog"
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
                appearance="catalog"
                aria-label={t("search")}
                placeholder={t("search")}
                {...searchDraft}
              />
              <PageAction
                appearance="catalog"
                intent="edit"
                hitArea="comfortable"
                aria-label={t("settingsAction")}
                asChild
              >
                <Link to={partnerReviewSettingsHref(wakerId)}>
                  <Settings />
                </Link>
              </PageAction>
              <PageAction
                appearance="catalog"
                intent="primary"
                className="shrink-0 whitespace-nowrap"
                onClick={() => setFormTarget(null)}
              >
                <Plus />
                {t("addAction")}
              </PageAction>
            </>
          }
        />

        <div className="mt-5 grid gap-6">
          {showCoreGroup ? (
            <MemoryGroup
              {...groupActions}
              sectionRef={coreGroup}
              title={t("core.title")}
              empty={t("manager.noResults")}
              items={coreItems}
              query={core}
              page={corePage}
              pageSize={PAGE_SIZE}
              onPageChange={(page) => changePage("corePage", page)}
            />
          ) : null}
          <MemoryGroup
            {...groupActions}
            sectionRef={regularGroup}
            title={t("regular.title")}
            empty={t(filtered ? "manager.noResults" : "regular.empty")}
            items={regularItems}
            query={regular}
            page={regularPage}
            pageSize={PAGE_SIZE}
            onPageChange={(page) => changePage("regularPage", page)}
          />
        </div>
      </ResourcePageSurface>

      {formTarget !== undefined ? (
        <MemoryFormDialog
          memory={formTarget}
          open
          wakerId={wakerId}
          onOpenChange={(open) => {
            if (!open) setFormTarget(undefined);
          }}
        />
      ) : null}
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
                deleteMemory.mutate(
                  { path: { memoryId: deleteTarget.id, wakerId } },
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
