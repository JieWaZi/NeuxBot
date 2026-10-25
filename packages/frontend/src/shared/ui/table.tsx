import type {
  HTMLAttributes,
  TableHTMLAttributes,
  TdHTMLAttributes,
  ThHTMLAttributes,
} from "react";

import { cn } from "@/shared/lib/cn";

type TableProps = TableHTMLAttributes<HTMLTableElement> & {
  containerClassName?: string;
};

export function Table({ className, containerClassName, ...props }: TableProps) {
  return (
    <div
      className={cn(
        "border-border/70 bg-surface-elevated w-full overflow-x-auto rounded-lg border",
        containerClassName,
      )}
    >
      <table
        className={cn("type-body w-full border-collapse", className)}
        {...props}
      />
    </div>
  );
}

export function TableHeader({
  className,
  ...props
}: HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn("bg-muted/60", className)} {...props} />;
}

export function TableBody({
  className,
  ...props
}: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <tbody
      className={cn("[&_tr:last-child]:border-b-0", className)}
      {...props}
    />
  );
}

export function TableRow({
  className,
  ...props
}: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={cn(
        "border-border/70 h-row-default border-b transition-colors",
        className,
      )}
      {...props}
    />
  );
}

export function TableHead({
  className,
  ...props
}: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        "text-muted-foreground type-ui h-row-default px-3 text-left font-medium whitespace-nowrap",
        className,
      )}
      {...props}
    />
  );
}

export function TableCell({
  className,
  ...props
}: TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn("px-3 py-2 align-middle", className)} {...props} />;
}

type TableEmptyRowsProps = {
  columnCount: number;
  count: number;
};

export function TableEmptyRows({ columnCount, count }: TableEmptyRowsProps) {
  return Array.from({ length: Math.max(0, count) }, (_, index) => (
    <TableRow
      key={`empty-row-${index}`}
      aria-hidden="true"
      data-table-empty-row
      className={index === count - 1 ? "border-b-0" : undefined}
    >
      <TableCell colSpan={columnCount} />
    </TableRow>
  ));
}
