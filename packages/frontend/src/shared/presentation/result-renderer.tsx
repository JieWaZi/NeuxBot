import {
  createContext,
  useContext,
  useMemo,
  useState,
  type ComponentType,
} from "react";
import { createStateStore } from "@json-render/core";
import {
  defineRegistry,
  Renderer,
  RepeatScopeProvider,
  JSONUIProvider,
} from "@json-render/react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import { Card } from "@/shared/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import {
  ArrowUpRight,
  ChevronDown,
  Info,
  AlertTriangle,
  CircleCheck,
  FileDown,
} from "lucide-react";
import { cn } from "@/shared/lib/cn";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import { openExternalURL } from "@/shared/platform";
import { withinResultBudget, resultDataPaths } from "./result-budget";
import { resultCatalog, type PageDefinition } from "./catalog";

export type PresentationFile = {
  id?: string;
  field?: string;
  name?: string;
  available?: boolean;
};
type ResultEnvironment = {
  compact: boolean;
  dataPaths: WeakMap<object, string>;
  Markdown: ComponentType<{ source: string }>;
  files: PresentationFile[];
  filePaths: Record<string, string>;
  download: (file: PresentationFile) => void;
};
const Environment = createContext<ResultEnvironment | null>(null);
function useEnvironment() {
  const context = useContext(Environment);
  if (!context) throw new Error("Missing result environment");
  return context;
}
function text(value: unknown) {
  return value === undefined
    ? "—"
    : typeof value === "string"
      ? value
      : (JSON.stringify(value) ?? "null");
}
function LongContent({
  value,
  markdown = false,
  className = "type-body",
}: {
  value: unknown;
  markdown?: boolean;
  className?: string;
}) {
  const { t } = useTranslation("workflow");
  const { compact, Markdown } = useEnvironment();
  const source = text(value);
  const long = compact && source.length > 800;
  const content = (content: string) =>
    markdown ? (
      <Markdown source={content} />
    ) : (
      <p className={cn(className, "break-words whitespace-pre-wrap")}>
        {content}
      </p>
    );
  return long ? (
    <details>
      <summary className="cursor-pointer list-none space-y-2">
        <span
          className={cn(className, "block break-words whitespace-pre-wrap")}
        >
          {source.slice(0, 300)}…
        </span>
        <span className="text-primary type-caption block">
          {t("resultPage.expand")}
        </span>
      </summary>
      {content(source)}
    </details>
  ) : (
    content(source)
  );
}
function Pagination({
  count,
  page,
  size,
  onChange,
}: {
  count: number;
  page: number;
  size: number;
  onChange: (page: number) => void;
}) {
  const { t } = useTranslation("workflow");
  if (count <= size) return null;
  return (
    <div className="flex items-center justify-end gap-3">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        disabled={page === 0}
        onClick={() => onChange(page - 1)}
      >
        {t("resultPage.previous")}
      </Button>
      <span className="text-muted-foreground type-caption">
        {page + 1} / {Math.ceil(count / size)}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        disabled={(page + 1) * size >= count}
        onClick={() => onChange(page + 1)}
      >
        {t("resultPage.next")}
      </Button>
    </div>
  );
}
// 只映射 Catalog 中的设计属性，Agent 组合这些能力生成页面；不解释任意 CSS。
const gaps = { none: "gap-0", small: "gap-2", medium: "gap-4", large: "gap-6" };
const alignment = {
  start: "items-start",
  center: "items-center",
  end: "items-end",
  stretch: "items-stretch",
};
const textAlignment = {
  start: "text-start",
  center: "text-center",
  end: "text-end",
};
const surfaces = {
  default: "bg-card text-foreground",
  accent: "bg-accent-surface text-foreground",
  info: "bg-status-info-surface text-foreground",
  success: "bg-status-success-surface text-foreground",
  warning: "bg-status-warning-surface text-foreground",
  danger: "bg-status-danger-surface text-foreground",
};
function gridColumns(columns: number | undefined) {
  return columns === 4
    ? "@md:grid-cols-2 @3xl:grid-cols-4"
    : columns === 3
      ? "@md:grid-cols-2 @2xl:grid-cols-3"
      : columns === 2
        ? "@md:grid-cols-2"
        : "grid-cols-1";
}
const { registry } = defineRegistry(resultCatalog, {
  components: {
    Page: ({ props, children }) => (
      <div
        data-presentation="page"
        className={cn(
          "grid w-full min-w-0",
          gaps[props.gap ?? "large"],
          props.width === "full"
            ? undefined
            : props.width === "wide"
              ? "max-w-5xl"
              : "max-w-3xl",
          props.align === "start" ? undefined : "mx-auto",
        )}
      >
        {children}
      </div>
    ),
    Stack: ({ props, children }) => (
      <div
        className={cn(
          "flex min-w-0",
          props.direction === "horizontal" ? "flex-row flex-wrap" : "flex-col",
          gaps[props.gap ?? "medium"],
          alignment[props.align ?? "stretch"],
          props.justify === "between"
            ? "justify-between"
            : props.justify === "center"
              ? "justify-center"
              : props.justify === "end"
                ? "justify-end"
                : "justify-start",
        )}
      >
        {children}
      </div>
    ),
    Grid: ({ props, children }) => (
      <div
        className={cn(
          "grid min-w-0",
          gaps[props.gap ?? "medium"],
          alignment[props.align ?? "stretch"],
          props.columns === 2 && props.ratio === "wide-start"
            ? "@xl:grid-cols-[2fr_1fr]"
            : props.columns === 2 && props.ratio === "wide-end"
              ? "@xl:grid-cols-[1fr_2fr]"
              : gridColumns(props.columns),
        )}
      >
        {children}
      </div>
    ),
    Section: ({ props, children }) => (
      <section className={cn("grid min-w-0", gaps[props.gap ?? "medium"])}>
        {props.title || props.description ? (
          <div className="grid gap-2">
            {props.title ? (
              <h2 className="type-title font-semibold">{text(props.title)}</h2>
            ) : null}
            {props.description ? (
              <p className="text-muted-foreground type-body">
                {text(props.description)}
              </p>
            ) : null}
          </div>
        ) : null}
        {children}
      </section>
    ),
    Card: ({ props, children }) => (
      <Card
        variant="section"
        className={cn("grid gap-4", surfaces[props.tone ?? "default"])}
      >
        {children}
      </Card>
    ),
    Heading: ({ props }) => {
      const Heading = props.level ?? "h2";
      return (
        <Heading
          className={cn(
            "min-w-0 font-semibold break-words",
            Heading === "h1"
              ? "type-page-title"
              : Heading === "h3"
                ? "type-label"
                : "type-title",
            textAlignment[props.align ?? "start"],
          )}
        >
          {text(props.value)}
        </Heading>
      );
    },
    Callout: ({ props }) => {
      const Icon =
        props.tone === "warning" || props.tone === "danger"
          ? AlertTriangle
          : props.tone === "success"
            ? CircleCheck
            : Info;
      return (
        <Card
          asChild
          variant="section"
          className={cn(
            "flex items-start gap-3",
            surfaces[props.tone ?? "info"],
          )}
        >
          <aside data-presentation="callout">
            <Icon className="size-icon-lg mt-0.5 shrink-0" />
            <div className="grid min-w-0 gap-2">
              {props.title ? (
                <p className="type-label font-semibold">{text(props.title)}</p>
              ) : null}
              <LongContent value={props.value} />
            </div>
          </aside>
        </Card>
      );
    },
    Disclosure: ({ props, children }) => (
      <Collapsible
        defaultOpen={props.defaultOpen ?? false}
        className="grid min-w-0 gap-3"
      >
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm" className="w-fit">
            <ChevronDown />
            {text(props.title)}
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="grid gap-4">{children}</div>
        </CollapsibleContent>
      </Collapsible>
    ),
    Text: ({ props }) => (
      <div
        className={
          props.tone === "muted"
            ? "text-muted-foreground"
            : props.tone === "accent"
              ? "text-primary"
              : undefined
        }
      >
        <LongContent
          value={props.value}
          className={cn(
            props.size === "caption"
              ? "type-caption"
              : props.size === "label"
                ? "type-label"
                : "type-body",
            props.weight === "semibold"
              ? "font-semibold"
              : props.weight === "medium"
                ? "font-medium"
                : "font-normal",
            textAlignment[props.align ?? "start"],
          )}
        />
      </div>
    ),
    Markdown: ({ props }) => <LongContent value={props.value} markdown />,
    KeyValue: ({ props }) => (
      <div className="grid min-w-0 gap-1">
        <span className="text-muted-foreground type-caption">
          {text(props.label)}
        </span>
        <LongContent value={props.value} />
      </div>
    ),
    Metric: ({ props }) => (
      <Card
        variant="section"
        className={cn("grid gap-2", surfaces[props.tone ?? "default"])}
      >
        <span className="text-muted-foreground type-caption">
          {text(props.label)}
        </span>
        <span className="type-display font-semibold tabular-nums">
          {text(props.value)}{" "}
          {props.unit ? (
            <span className="text-muted-foreground type-body font-normal">
              {text(props.unit)}
            </span>
          ) : null}
        </span>
        {props.description ? (
          <p className="text-muted-foreground type-body">
            {text(props.description)}
          </p>
        ) : null}
      </Card>
    ),
    Badge: ({ props }) => (
      <Badge
        variant={
          props.tone === "success"
            ? "success"
            : props.tone === "warning"
              ? "warning"
              : props.tone === "danger"
                ? "destructive"
                : "secondary"
        }
      >
        {text(props.value)}
      </Badge>
    ),
    Link: ({ props }) => {
      const href =
        typeof props.href === "string" && /^https?:\/\//i.test(props.href)
          ? props.href
          : undefined;
      return href && props.variant === "button" ? (
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="w-fit"
          onClick={() => void openExternalURL(href)}
        >
          {text(props.label)}
          <ArrowUpRight />
        </Button>
      ) : href ? (
        <a
          href={href}
          className="text-link type-body focus-visible:ring-ring inline-flex w-fit min-w-0 items-center gap-1 rounded-sm underline underline-offset-4 focus-visible:ring-2 focus-visible:outline-none"
          onClick={(event) => {
            event.preventDefault();
            void openExternalURL(href);
          }}
        >
          {text(props.label)}
          <ArrowUpRight className="size-icon-sm shrink-0" />
        </a>
      ) : (
        <span className="text-muted-foreground type-body">
          {text(props.label)}
        </span>
      );
    },
    Table: function ResultTable({ props }) {
      const [selected, setPage] = useState(0);
      const { compact } = useEnvironment();
      const rows = Array.isArray(props.rows) ? props.rows : [];
      const size = props.pageSize ?? (compact ? 5 : 20);
      const page = Math.min(
        selected,
        Math.max(0, Math.ceil(rows.length / size) - 1),
      );
      return (
        <div className="grid min-w-0 gap-2">
          <Table>
            <TableHeader>
              <TableRow>
                {props.columns.map((column) => (
                  <TableHead key={column.key}>{column.label}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.slice(page * size, (page + 1) * size).map((row, index) => (
                <TableRow key={page * size + index}>
                  {props.columns.map((column) => (
                    <TableCell key={column.key}>
                      <LongContent
                        value={
                          row && typeof row === "object"
                            ? (row as Record<string, unknown>)[column.key]
                            : undefined
                        }
                      />
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <Pagination
            count={rows.length}
            page={page}
            size={size}
            onChange={setPage}
          />
        </div>
      );
    },
    Collection: function ResultCollection({ props, children }) {
      const [selected, setPage] = useState(0);
      const { compact, dataPaths } = useEnvironment();
      const items = Array.isArray(props.items) ? props.items : [];
      const size = props.pageSize ?? (compact ? 3 : 10);
      const page = Math.min(
        selected,
        Math.max(0, Math.ceil(items.length / size) - 1),
      );
      // 原生 scope 负责所有 $item/$index 解析；只挂载当前页，保留完整 /result。
      return (
        <div className="grid min-w-0 gap-4">
          <div
            className={cn(
              "grid min-w-0",
              gaps[props.gap ?? "medium"],
              props.layout === "grid"
                ? gridColumns(props.columns ?? 2)
                : "grid-cols-1",
            )}
          >
            {items.slice(page * size, (page + 1) * size).map((item, index) => (
              <div
                key={page * size + index}
                className="grid min-w-0 content-start gap-4"
              >
                <RepeatScopeProvider
                  item={item}
                  index={page * size + index}
                  basePath={`${dataPaths.get(items) ?? "/result"}/${page * size + index}`}
                >
                  {children}
                </RepeatScopeProvider>
              </div>
            ))}
          </div>
          <Pagination
            count={items.length}
            page={page}
            size={size}
            onChange={setPage}
          />
        </div>
      );
    },
    FileList: function ResultFiles({ props }) {
      const { files, filePaths, download } = useEnvironment();
      const paths = Array.isArray(props.files) ? props.files : [props.files];
      return (
        <div className="grid gap-2">
          {files
            .filter((file) => paths.includes(filePaths[file.field ?? ""]))
            .map((file) => (
              <Button
                key={file.id}
                type="button"
                variant="secondary"
                size="sm"
                className="w-fit max-w-full"
                disabled={!file.available}
                onClick={() => download(file)}
              >
                <FileDown />
                <span className="truncate">
                  {props.label ? text(props.label) : file.name}
                </span>
              </Button>
            ))}
        </div>
      );
    },
  },
});

/** 原生 Renderer 共享页面与消息渲染；每个宿主有独立展开和分页状态。 */
export function ResultRenderer({
  page,
  result,
  compact = false,
  Markdown,
  files = [],
  filePaths = {},
  download,
}: {
  page: PageDefinition;
  result: unknown;
  compact?: boolean;
  Markdown: ComponentType<{ source: string }>;
  files?: PresentationFile[];
  filePaths?: Record<string, string>;
  download: (file: PresentationFile) => void;
}) {
  const { t } = useTranslation("workflow");
  const validBudget = useMemo(
    () => withinResultBudget(page.spec, result),
    [page.spec, result],
  );
  const store = useMemo(() => createStateStore({ result }), [result]);
  const dataPaths = useMemo(() => resultDataPaths(result), [result]);
  const environment = useMemo(
    () => ({ compact, Markdown, files, filePaths, download, dataPaths }),
    [compact, Markdown, files, filePaths, download, dataPaths],
  );
  if (!validBudget)
    return (
      <p role="alert" className="text-destructive type-body">
        {t("resultPage.budgetExceeded")}
      </p>
    );
  return (
    <Environment.Provider value={environment}>
      <JSONUIProvider store={store} registry={registry}>
        <div className="@container min-w-0">
          <Renderer spec={page.spec} registry={registry} />
        </div>
      </JSONUIProvider>
    </Environment.Provider>
  );
}
