import type { TableHTMLAttributes } from "react";

import { cn } from "@/shared/lib/cn";
import { Pagination, type PaginationProps } from "@/shared/ui/pagination";
import { Table } from "@/shared/ui/table";

type PaginatedTableProps = TableHTMLAttributes<HTMLTableElement> & {
  pagination?: PaginationProps;
};

export function PaginatedTable({
  className,
  pagination,
  ...tableProps
}: PaginatedTableProps) {
  return (
    <div className="flex flex-col">
      <Table
        containerClassName="overflow-auto [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
        className={cn(className)}
        {...tableProps}
      />
      {pagination ? (
        <div className="mt-4 shrink-0">
          <Pagination {...pagination} />
        </div>
      ) : null}
    </div>
  );
}
