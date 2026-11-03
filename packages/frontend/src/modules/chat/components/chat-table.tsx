import { Copy, Download, Maximize2 } from "lucide-react";
import {
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type ReactNode,
  type Ref,
} from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

import {
  copyPlainText,
  downloadTextFile,
  serializeTable,
  type TableExportFormat,
} from "./chat-markdown-export";
import { ChatPreviewDialog } from "./chat-preview-dialog";

type ChatTableProps = ComponentPropsWithoutRef<"table"> & {
  node?: unknown;
};

const tableFormats: readonly TableExportFormat[] = ["markdown", "csv", "tsv"];

const tableClassName = cn(
  "type-body w-max max-w-max table-auto border-collapse [&_thead]:bg-muted/60 [&_th]:type-body [&_th]:text-muted-foreground [&_th]:px-3 [&_th]:py-1.5 [&_th:not([data-markdown-align])]:text-left [&_th]:font-medium [&_td]:px-3 [&_td]:py-1.5 [&_td]:align-middle [&_tr]:border-border/70 [&_tr]:border-b [&_tr:last-child]:border-b-0 [&_th]:min-w-24 [&_th]:max-w-xs [&_th]:break-words [&_th]:whitespace-normal [&_td]:min-w-24 [&_td]:max-w-xs [&_td]:break-words",
  // 比较矩阵保留自然宽度；短表及引用内的表填满所属内容列。
  "[&:not(:has(th:nth-child(4),td:nth-child(4)))]:w-full [&:not(:has(th:nth-child(4),td:nth-child(4)))]:max-w-none [blockquote_&]:w-full [blockquote_&]:max-w-none",
);

function TableFrame({
  children,
  className,
  preview = false,
  tableRef,
  ...props
}: ChatTableProps & {
  preview?: boolean;
  tableRef?: Ref<HTMLTableElement>;
}) {
  const { t } = useTranslation("chat");
  return (
    <div
      role="region"
      aria-label={t("tableTitle")}
      tabIndex={0}
      className={cn(
        "border-border/70 focus-visible:ring-ring w-full max-w-full min-w-0 overflow-x-auto overscroll-x-contain rounded-sm border outline-none focus-visible:ring-2",
        preview &&
          "[&_thead]:bg-muted max-h-[calc(100dvh-144px)] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10",
      )}
    >
      <table
        ref={tableRef}
        className={cn(className, tableClassName)}
        data-streamdown="table"
        {...props}
      >
        {children}
      </table>
    </div>
  );
}

function TableFormatMenu({
  icon,
  label,
  onSelect,
}: {
  icon: ReactNode;
  label: string;
  onSelect: (format: TableExportFormat) => void;
}) {
  const { t } = useTranslation("chat");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={label}
          aria-label={label}
        >
          {icon}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {tableFormats.map((format) => (
          <DropdownMenuItem key={format} onSelect={() => onSelect(format)}>
            {t(`tableFormat.${format}`)}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function ChatTable({
  children,
  className,
  node: _node,
  ...props
}: ChatTableProps) {
  void _node;
  const { t } = useTranslation("chat");
  const tableRef = useRef<HTMLTableElement>(null);
  const [previewOpen, setPreviewOpen] = useState(false);

  const copyFormat = async (format: TableExportFormat) => {
    const table = tableRef.current;
    if (!table) return;
    await copyPlainText(serializeTable(table, format).content);
  };

  const downloadFormat = async (format: TableExportFormat) => {
    const table = tableRef.current;
    if (!table) return;
    const payload = serializeTable(table, format);
    await downloadTextFile(payload.fileName, payload.content, payload.mimeType);
  };

  return (
    <section
      className="group/table relative my-2 flex w-full max-w-none min-w-0 flex-col self-stretch"
      data-streamdown="table-wrapper"
    >
      <header className="bg-popover duration-quick pointer-events-none absolute top-1 right-1 z-20 flex items-center gap-1 rounded-md opacity-0 transition-opacity group-focus-within/table:pointer-events-auto group-focus-within/table:opacity-100 group-hover/table:pointer-events-auto group-hover/table:opacity-100 motion-reduce:transition-none [&:has([data-state=open])]:pointer-events-auto [&:has([data-state=open])]:opacity-100 [@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100">
        <TableFormatMenu
          icon={<Copy />}
          label={t("tableCopy")}
          onSelect={(format) => void copyFormat(format)}
        />
        <TableFormatMenu
          icon={<Download />}
          label={t("tableDownload")}
          onSelect={(format) => void downloadFormat(format)}
        />
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={t("previewFullscreen")}
          aria-label={t("previewFullscreen")}
          onClick={() => setPreviewOpen(true)}
        >
          <Maximize2 />
        </Button>
      </header>
      <TableFrame className={className} tableRef={tableRef} {...props}>
        {children}
      </TableFrame>
      <ChatPreviewDialog
        open={previewOpen}
        title={t("tableTitle")}
        onOpenChange={setPreviewOpen}
      >
        <TableFrame className={className} preview {...props}>
          {children}
        </TableFrame>
      </ChatPreviewDialog>
    </section>
  );
}
