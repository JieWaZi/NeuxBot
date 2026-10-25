import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";

import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

type PaginationGap = "start-gap" | "end-gap";

export type PaginationItem = number | PaginationGap;

export type PaginationLabels = {
  navigation: string;
  next: string;
  page: (page: number) => string;
  pageSize: (pageSize: number) => string;
  pageSizeSelection: string;
  previous: string;
  total: (totalItems: number) => string;
};

export type PaginationProps = {
  labels: PaginationLabels;
  onPageChange: (page: number) => void;
  onPageSizeChange?: (pageSize: number) => void;
  page: number;
  pageSize: number;
  pageSizeOptions?: readonly number[];
  pending?: boolean;
  totalItems: number;
};

export function paginationItems(page: number, totalPages: number) {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, index) => index + 1);
  }
  if (page <= 4) {
    return [1, 2, 3, 4, 5, "end-gap", totalPages] satisfies PaginationItem[];
  }
  if (page >= totalPages - 3) {
    return [
      1,
      "start-gap",
      totalPages - 4,
      totalPages - 3,
      totalPages - 2,
      totalPages - 1,
      totalPages,
    ] satisfies PaginationItem[];
  }
  return [
    1,
    "start-gap",
    page - 1,
    page,
    page + 1,
    "end-gap",
    totalPages,
  ] satisfies PaginationItem[];
}

export function Pagination({
  labels,
  onPageChange,
  onPageSizeChange,
  page,
  pageSize,
  pageSizeOptions,
  totalItems,
  pending = false,
}: PaginationProps) {
  const normalizedPageSize = Math.max(1, pageSize);
  const normalizedTotalItems = Math.max(0, totalItems);
  const totalPages = Math.max(
    1,
    Math.ceil(normalizedTotalItems / normalizedPageSize),
  );
  const currentPage = Math.min(Math.max(page, 1), totalPages);
  const items = paginationItems(currentPage, totalPages);

  return (
    <div className="min-h-control-sm flex items-center justify-between gap-4">
      <div className="flex items-center gap-2">
        <p className="text-muted-foreground type-ui">
          {labels.total(normalizedTotalItems)}
        </p>
        {pageSizeOptions && onPageSizeChange ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={labels.pageSizeSelection}
              >
                {labels.pageSize(normalizedPageSize)}
                <ChevronDown />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuRadioGroup
                value={String(normalizedPageSize)}
                onValueChange={(value) => onPageSizeChange(Number(value))}
              >
                {pageSizeOptions.map((option) => (
                  <DropdownMenuRadioItem key={option} value={String(option)}>
                    {labels.pageSize(option)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
      <nav aria-label={labels.navigation} className="flex items-center gap-1">
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={labels.previous}
          disabled={currentPage <= 1 || pending}
          onClick={() => onPageChange(currentPage - 1)}
        >
          <ChevronLeft />
        </Button>
        {items.map((item) =>
          typeof item === "number" ? (
            <Button
              key={item}
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-current={item === currentPage ? "page" : undefined}
              aria-label={labels.page(item)}
              disabled={pending}
              className={
                item === currentPage ? "bg-muted text-foreground" : undefined
              }
              onClick={() => onPageChange(item)}
            >
              {item}
            </Button>
          ) : (
            <span
              key={item}
              aria-hidden="true"
              className="text-muted-foreground type-ui size-control-sm grid place-items-center"
            >
              …
            </span>
          ),
        )}
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={labels.next}
          disabled={currentPage >= totalPages || pending}
          onClick={() => onPageChange(currentPage + 1)}
        >
          <ChevronRight />
        </Button>
      </nav>
    </div>
  );
}
