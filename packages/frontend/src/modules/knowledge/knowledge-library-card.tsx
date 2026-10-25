import {
  BookOpen,
  Cloud,
  HardDrive,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Card, CardFooter } from "@/shared/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import type { Library } from "./knowledge-model";
import { useCopy } from "./use-copy";

export function KnowledgeLibraryCard({
  library,
  open,
  onDelete,
  canDelete,
}: {
  library: Library;
  open: (library: Library) => void;
  onDelete?: (library: Library) => void;
  canDelete: boolean;
}) {
  const t = useCopy();
  const { i18n } = useTranslation("knowledge");
  const observedAt = library.pageTotalObservedAt
    ? new Date(library.pageTotalObservedAt)
    : undefined;
  const countHelp = t("remoteWikiCountHelp", {
    date:
      observedAt && Number.isFinite(observedAt.getTime())
        ? new Intl.DateTimeFormat(i18n.language, {
            dateStyle: "medium",
            timeStyle: "short",
          }).format(observedAt)
        : t("unknown"),
  });
  const count =
    library.pageTotal === undefined
      ? t("wikiCountUnknown")
      : t("pageCount", { count: library.pageTotal });
  const canRemove =
    canDelete && (library.storage === "local" || Boolean(library.mountId));
  const StorageIcon = library.storage === "remote" ? Cloud : HardDrive;
  const moreActions = t("libraryMoreActions", { name: library.name });
  return (
    <>
      <Card asChild variant="resource">
        <article>
          <button
            type="button"
            aria-label={library.name}
            disabled={library.offline}
            onClick={() => open(library)}
            className="hover:bg-surface-subtle focus-visible:ring-ring duration-quick grid min-h-32 w-full min-w-0 grid-cols-[40px_minmax(0,1fr)] items-start gap-4 p-5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:cursor-not-allowed disabled:opacity-60 motion-reduce:transition-none"
          >
            <BookOpen
              className="text-muted-foreground size-icon-lg"
              aria-hidden="true"
              strokeWidth={1.5}
            />
            <div className="min-w-0">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2
                  className="type-label min-w-0 truncate font-medium"
                  title={library.name}
                >
                  {library.name}
                </h2>
                <Badge variant="secondary">
                  <StorageIcon aria-hidden="true" />
                  {t(library.storage)}
                </Badge>
                {library.storage === "remote" && (
                  <Badge variant={library.offline ? "destructive" : "success"}>
                    {t(library.offline ? "offline" : "online")}
                  </Badge>
                )}
              </div>
              {library.description ? (
                <p className="text-muted-foreground type-body mt-2 line-clamp-3 wrap-anywhere">
                  {library.description}
                </p>
              ) : null}
            </div>
          </button>
          <CardFooter className="grid grid-cols-[minmax(0,1fr)_44px] gap-0 px-0">
            <div className="text-muted-foreground type-caption flex min-w-0 items-center gap-1 px-5">
              <BookOpen className="size-icon-xs" aria-hidden="true" />
              {library.storage === "remote" &&
              library.pageTotal !== undefined ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span tabIndex={0} className="cursor-help">
                      {count}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{countHelp}</TooltipContent>
                </Tooltip>
              ) : (
                <span>{count}</span>
              )}
            </div>
            <DropdownMenu>
              <Tooltip>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type="button"
                      aria-label={moreActions}
                      className="mx-auto"
                      size="icon"
                      variant="ghost"
                    >
                      <MoreHorizontal aria-hidden="true" strokeWidth={1.5} />
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent>{moreActions}</TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  disabled={!canRemove || !onDelete}
                  onSelect={() => onDelete?.(library)}
                >
                  <Trash2 aria-hidden="true" />
                  {t(library.mountId ? "removeRemoteMount" : "deleteLibrary")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </CardFooter>
        </article>
      </Card>
    </>
  );
}
