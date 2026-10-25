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

const tableClassName =
  "type-body w-full border-collapse [&_thead]:bg-muted/60 [&_th]:text-muted-foreground [&_th]:type-ui [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:font-medium [&_th]:whitespace-nowrap [&_td]:px-3 [&_td]:py-2 [&_td]:align-middle [&_tr]:border-border/70 [&_tr]:border-b [&_tr:last-child]:border-b-0";

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
  return (
    <div
      className={cn(
        "border-border bg-surface-elevated overflow-auto rounded-lg border",
        preview &&
          "[&_thead]:bg-muted max-h-[calc(100dvh-144px)] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10",
      )}
    >
      <table
        ref={tableRef}
        className={cn(tableClassName, className)}
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
      className="border-border bg-surface-elevated my-3 rounded-lg border p-3"
      data-streamdown="table-wrapper"
    >
      <header className="mb-3 flex items-center justify-end gap-1">
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
