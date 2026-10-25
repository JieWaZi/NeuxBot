import { KnowledgeLoading } from "./knowledge-loading";
import { lazy, Suspense, useState } from "react";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { BookOpen, Plus, Cloud } from "lucide-react";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import {
  PageAction,
  ResourceCardGrid,
  ResourcePageHeader,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { useCopy } from "./use-copy";
import { type Library } from "./knowledge-model";
import { Empty } from "./knowledge-controls";
import { KnowledgeLibraryCard } from "./knowledge-library-card";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";

const KnowledgeLibraryDeleteDialog = lazy(() =>
  import("./knowledge-library-delete-dialog").then((module) => ({
    default: module.KnowledgeLibraryDeleteDialog,
  })),
);

export function LibraryList({
  libraries,
  open,
  create,
  canCreate = true,
  onAddRemote,
  state,
  error,
  onRetry,
  search = "",
  onSearchChange,
  location = "all",
  onLocationChange,
  total,
  onDelete,
  canDelete = () => true,
}: {
  libraries: Library[];
  onAddRemote?: () => void;
  onDelete?: (library: Library, requestId: string) => Promise<void>;
  canDelete?: (library: Library) => boolean;
  state?: "loading" | "error";
  error?: unknown;
  onRetry?: () => void;
  search?: string;
  onSearchChange?: (search: string) => void;
  open: (library: Library) => void;
  location?: "all" | "local" | "remote";
  onLocationChange?: (location: "all" | "local" | "remote") => void;
  total?: number;
  create: () => void;
  canCreate?: boolean;
}) {
  const t = useCopy();
  const active = useRouteSceneActive();
  const [deleting, setDeleting] = useState<Library | null>(null);
  return (
    <ResourcePageSurface contentClassName="pt-6">
      <ResourcePageHeader
        title={t("knowledge")}
        description={t("intro")}
        actions={
          <>
            {onAddRemote && (
              <PageAction
                variant="plain"
                hitArea="comfortable"
                aria-label={t("addRemote")}
                onClick={onAddRemote}
              >
                <Cloud />
              </PageAction>
            )}
            {canCreate && (
              <PageAction intent="primary" label={t("create")} onClick={create}>
                <Plus />
              </PageAction>
            )}
          </>
        }
      />
      <ResourcePageToolbar
        filters={
          <>
            <ResourceFilterSelect
              ariaLabel={t("storageLocation")}
              value={location}
              onValueChange={(value) => {
                if (value === "all" || value === "local" || value === "remote")
                  onLocationChange?.(value);
              }}
              options={[
                { value: "all", label: t("all") },
                { value: "local", label: t("local") },
                { value: "remote", label: t("remote") },
              ]}
            />
            <ResourcePageSearchField
              aria-label={t("librarySearch")}
              placeholder={t("librarySearch")}
              value={search}
              onChange={(event) => onSearchChange?.(event.target.value)}
            />
          </>
        }
      >
        <div className="flex items-center gap-3 whitespace-nowrap">
          <b>{t("libraries")}</b>
          <span className="type-caption text-muted-foreground">
            {t("libraryCount", {
              count: total ?? libraries.length,
            })}
          </span>
        </div>
      </ResourcePageToolbar>
      <KnowledgeReadFeedback
        hasData={Boolean(libraries.length)}
        state={{
          pending: false,
          error: state === "error" ? (error ?? true) : undefined,
          retry: () => onRetry?.(),
        }}
      />
      {state === "loading" && !libraries.length ? (
        <KnowledgeLoading />
      ) : libraries.length ? (
        <ResourceCardGrid className="mt-6" aria-label={t("libraries")}>
          {libraries.map((library) => (
            <KnowledgeLibraryCard
              key={JSON.stringify([
                library.ref.backend,
                library.ref.connectionId,
                library.ref.knowledgeBaseId,
              ])}
              library={library}
              open={open}
              onDelete={onDelete ? setDeleting : undefined}
              canDelete={canDelete(library)}
            />
          ))}
        </ResourceCardGrid>
      ) : state !== "error" ? (
        <Empty
          icon={BookOpen}
          title={t("emptyLibraries")}
          description={t("emptyLibrariesHelp")}
        />
      ) : null}
      {deleting && onDelete ? (
        <Suspense fallback={null}>
          <KnowledgeLibraryDeleteDialog
            key={JSON.stringify([
              deleting.ref.backend,
              deleting.ref.connectionId,
              deleting.ref.knowledgeBaseId,
            ])}
            library={deleting}
            open={active}
            onOpenChange={(value) => {
              if (!value) setDeleting(null);
            }}
            onDelete={onDelete}
          />
        </Suspense>
      ) : null}
    </ResourcePageSurface>
  );
}
