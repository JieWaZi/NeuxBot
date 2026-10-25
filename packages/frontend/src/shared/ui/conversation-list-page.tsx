import { MessageCircle } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { EmptyState } from "@/shared/ui/empty-state";
import { PaginatedTable } from "@/shared/ui/paginated-table";
import { PageHeader } from "@/shared/ui/page-header";
import type { PaginationProps } from "@/shared/ui/pagination";
import { SearchField } from "@/shared/ui/search-field";
import { Skeleton } from "@/shared/ui/skeleton";
import { Toolbar } from "@/shared/ui/toolbar";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";

export type ChatListItem = {
  href: string;
  id: string;
  status?: {
    badge?: boolean;
    label: string;
    tone: "active" | "idle" | "warning";
  };
  title: string;
  updatedAt?: number;
  workspace: string;
};

type ChatListLabels = {
  actions: string;
  empty: string;
  filteredEmpty: string;
  lastActive: string;
  loadError: string;
  name: string;
  retry: string;
  table: string;
  workspace: string;
};

type ChatListSelection = {
  allVisibleSelected: boolean;
  onSelectAll: (selected: boolean) => void;
  onSelectItem: (id: string, selected: boolean) => void;
  selectAllLabel: string;
  selectItemLabel: (item: ChatListItem) => string;
  selectedIds: ReadonlySet<string>;
  someVisibleSelected: boolean;
};

type ChatListTableProps = {
  error: boolean;
  filtered: boolean;
  footer?: ReactNode;
  items: ChatListItem[];
  labels: ChatListLabels;
  loading: boolean;
  onOpen: (item: ChatListItem) => void;
  onRetry: () => void;
  pageSize: number;
  pagination?: PaginationProps;
  renderActions: (item: ChatListItem) => ReactNode;
  selection?: ChatListSelection;
};

type ChatListPageProps = {
  description: string;
  headerAction?: ReactNode;
  invalidMessage?: string;
  query: {
    label: string;
    onChange: (value: string) => void;
    placeholder: string;
    value: string;
  };
  selectionActions?: ReactNode;
  table: ChatListTableProps;
  title: string;
  toolbarEnd?: ReactNode;
  toolbarNote?: ReactNode;
};

const activityTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

const statusClassName = {
  active: "bg-success animate-pulse",
  idle: "bg-border",
  warning: "bg-warning",
} as const;

function ChatListTable({
  error,
  filtered,
  footer,
  items,
  labels,
  loading,
  onOpen,
  onRetry,
  pageSize,
  pagination,
  renderActions,
  selection,
}: ChatListTableProps) {
  const columnCount = selection ? 5 : 4;
  return (
    <>
      <PaginatedTable
        aria-busy={loading || undefined}
        aria-label={labels.table}
        pagination={!loading && !error ? pagination : undefined}
      >
        <TableHeader>
          <TableRow>
            {selection ? (
              <TableHead className="w-12">
                <Checkbox
                  size="sm"
                  disabled={loading}
                  checked={
                    selection.allVisibleSelected
                      ? true
                      : selection.someVisibleSelected
                        ? "indeterminate"
                        : false
                  }
                  aria-label={selection.selectAllLabel}
                  onCheckedChange={(checked) =>
                    selection.onSelectAll(checked === true)
                  }
                />
              </TableHead>
            ) : null}
            <TableHead>{labels.name}</TableHead>
            <TableHead>{labels.workspace}</TableHead>
            <TableHead>{labels.lastActive}</TableHead>
            <TableHead className="w-12">
              <span className="sr-only">{labels.actions}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <>
              {Array.from({ length: Math.min(5, pageSize) }, (_, index) => (
                <TableRow key={index}>
                  {selection ? (
                    <TableCell className="w-12">
                      <Skeleton className="size-4 rounded-xs" />
                    </TableCell>
                  ) : null}
                  <TableCell>
                    <Skeleton className="h-4 w-40" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="h-4 w-32" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="h-4 w-32" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="size-control-sm" />
                  </TableCell>
                </TableRow>
              ))}
            </>
          ) : error ? (
            <TableRow>
              <TableCell colSpan={columnCount} className="p-0">
                <EmptyState
                  density="compact"
                  role="alert"
                  title={labels.loadError}
                  actions={
                    <Button size="sm" variant="secondary" onClick={onRetry}>
                      {labels.retry}
                    </Button>
                  }
                />
              </TableCell>
            </TableRow>
          ) : items.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columnCount} className="p-0">
                <EmptyState
                  density="compact"
                  icon={MessageCircle}
                  title={filtered ? labels.filteredEmpty : labels.empty}
                />
              </TableCell>
            </TableRow>
          ) : (
            items.map((item) => (
              <TableRow
                key={item.id}
                data-state={
                  selection?.selectedIds.has(item.id) ? "selected" : undefined
                }
                className="hover:bg-muted/55 data-[state=selected]:bg-muted/60 cursor-pointer"
                onClick={() => onOpen(item)}
              >
                {selection ? (
                  <TableCell
                    className="w-12"
                    onClick={(event) => event.stopPropagation()}
                  >
                    <Checkbox
                      size="sm"
                      checked={selection.selectedIds.has(item.id)}
                      aria-label={selection.selectItemLabel(item)}
                      onCheckedChange={(checked) =>
                        selection.onSelectItem(item.id, checked === true)
                      }
                    />
                  </TableCell>
                ) : null}
                <TableCell className="min-w-56">
                  <div className="flex min-w-0 items-center gap-2">
                    {item.status ? (
                      <>
                        <span
                          aria-hidden="true"
                          className={cn(
                            "size-1.5 shrink-0 rounded-full",
                            statusClassName[item.status.tone],
                          )}
                        />
                        <span className="sr-only">{item.status.label}</span>
                      </>
                    ) : null}
                    <Link
                      to={item.href}
                      className="truncate font-medium outline-none focus-visible:underline"
                      onClick={(event) => event.stopPropagation()}
                    >
                      {item.title}
                    </Link>
                    {item.status?.badge ? (
                      <span className="bg-warning/15 text-warning type-caption shrink-0 rounded-xs px-2 py-0.5">
                        {item.status.label}
                      </span>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="text-muted-foreground min-w-48">
                  <span className="block max-w-72 truncate">
                    {item.workspace}
                  </span>
                </TableCell>
                <TableCell className="text-muted-foreground whitespace-nowrap">
                  {item.updatedAt
                    ? activityTimeFormatter.format(new Date(item.updatedAt))
                    : "—"}
                </TableCell>
                <TableCell
                  className="w-12"
                  onClick={(event) => event.stopPropagation()}
                >
                  {renderActions(item)}
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </PaginatedTable>
      {!loading && !error ? footer : null}
    </>
  );
}

/**
 * 伙伴与团队会话列表共用的完整表现层；调用方只适配查询、路由和领域操作。
 */
export function ChatListPage({
  description,
  headerAction,
  invalidMessage,
  query,
  selectionActions,
  table,
  title,
  toolbarEnd,
  toolbarNote,
}: ChatListPageProps) {
  return (
    <ResourcePageSurface>
      <div className="w-full">
        <PageHeader
          title={title}
          description={description}
          actions={headerAction}
        />

        <div
          className={cn(
            "mb-3 shrink-0",
            selectionActions && "border-border-subtle border-b pb-3",
          )}
        >
          <Toolbar
            className={cn(selectionActions && "border-b-0 pb-0")}
            divided={!selectionActions}
            leading={
              <>
                <SearchField
                  size="sm"
                  width="toolbar"
                  aria-label={query.label}
                  placeholder={query.placeholder}
                  maxLength={120}
                  value={query.value}
                  onChange={(event) => query.onChange(event.target.value)}
                />
                {toolbarNote}
              </>
            }
            trailing={toolbarEnd}
          />
          {selectionActions}
        </div>

        <div className="min-w-0">
          {invalidMessage ? (
            <div
              role="alert"
              className="border-destructive/30 bg-destructive/8 text-destructive type-body rounded-lg border p-4"
            >
              {invalidMessage}
            </div>
          ) : (
            <ChatListTable {...table} />
          )}
        </div>
      </div>
    </ResourcePageSurface>
  );
}
