import { useId, type ComponentProps, type RefObject } from "react";
import { useTranslation } from "react-i18next";

import { MemoryList, MemoryListEmpty, MemoryListError } from "./memory-list";
import { Pagination } from "@/shared/ui/pagination";
import { QueryRefreshError } from "@/shared/ui/query-state";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";

type MemoryGroupProps = ComponentProps<typeof MemoryList> & {
  empty: string;
  page: number;
  pageSize: number;
  title: string;
  sectionRef: RefObject<HTMLElement | null>;
  onPageChange: (page: number) => void;
  query: {
    data?: { totalSize?: number };
    error: unknown;
    isFetching: boolean;
    isLoading: boolean;
    refetch: () => Promise<unknown>;
  };
};

export function MemoryGroup({
  empty,
  page,
  pageSize,
  title,
  sectionRef,
  onPageChange,
  query,
  ...listProps
}: MemoryGroupProps) {
  const { t } = useTranslation("memory");
  const heading = useId();
  const total = query.data?.totalSize ?? 0;
  const retry = () => void query.refetch();
  return (
    <section
      ref={sectionRef}
      aria-labelledby={heading}
      className="grid min-w-0 gap-3"
    >
      <h2
        id={heading}
        tabIndex={-1}
        className="type-title focus-visible:ring-ring w-fit rounded-sm font-medium focus-visible:ring-2 focus-visible:outline-none"
      >
        {title}
      </h2>
      {query.isLoading ? (
        <ResourceLoadingState layout="rows" />
      ) : !query.data && query.error ? (
        <MemoryListError text={t("loadError")} onRetry={retry} />
      ) : (
        <>
          {query.error ? (
            <QueryRefreshError
              error={query.error}
              pending={query.isFetching}
              onRetry={retry}
            />
          ) : null}
          {listProps.items.length ? (
            <MemoryList {...listProps} />
          ) : (
            <MemoryListEmpty text={empty} />
          )}
          {total > pageSize || page > 1 ? (
            <div className="max-w-full overflow-x-auto">
              <Pagination
                labels={{
                  navigation: title,
                  next: t("pagination.next"),
                  previous: t("pagination.previous"),
                  page: (page) => t("pagination.page", { page }),
                  pageSize: (size) => t("pagination.pageSize", { size }),
                  pageSizeSelection: t("pagination.pageSizeSelection"),
                  total: (count) => t("pagination.total", { count }),
                }}
                page={page}
                pageSize={pageSize}
                totalItems={total}
                pending={query.isFetching}
                onPageChange={onPageChange}
              />
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}
