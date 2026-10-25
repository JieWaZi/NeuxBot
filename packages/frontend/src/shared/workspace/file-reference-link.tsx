import {
  createContext,
  isValidElement,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy, Link2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/sonner";
import { openExternalURL } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { cn } from "@/shared/lib/cn";
import { WorkspaceFileTypeIcon } from "./workspace-file-type-icon";
import {
  emptyFileReferenceContext,
  fileReferenceLabel,
  localFileURL,
  parseReference,
  resolveFileReference,
  type FileReferenceCandidate,
  type FileReferenceContext,
  type Reference,
  type ResolvedFileReference,
} from "./file-reference";

type ReferenceNavigation = {
  context: FileReferenceContext;
  onOpenFile?: (file: ResolvedFileReference) => void;
  onOpenURL?: (url: string) => unknown;
};
const ReferenceContext = createContext<ReferenceNavigation>({
  context: emptyFileReferenceContext,
});

export function FileReferenceProvider({
  children,
  context,
  onOpenFile,
  onOpenURL,
}: ReferenceNavigation & { children: ReactNode }) {
  const navigation = useMemo(
    () => ({ context, onOpenFile, onOpenURL }),
    [context, onOpenFile, onOpenURL],
  );
  return (
    <ReferenceContext.Provider value={navigation}>
      {children}
    </ReferenceContext.Provider>
  );
}

function textOf(children: ReactNode): string {
  if (typeof children === "string" || typeof children === "number")
    return String(children);
  if (Array.isArray(children)) return children.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(children))
    return textOf(children.props.children);
  return "";
}

export function useFileReference(
  reference: FileReferenceCandidate | undefined,
  scope?: FileReferenceContext,
) {
  const navigation = useContext(ReferenceContext);
  const context = scope ?? navigation.context;
  return useQuery({
    queryKey: [
      "file-reference",
      context,
      reference?.path,
      reference?.line,
      reference?.column,
      reference?.endLine,
    ],
    queryFn: () =>
      reference
        ? resolveFileReference(reference, context)
        : Promise.resolve({ status: "unavailable" as const }),
    enabled: Boolean(reference && context.workspaceRoot),
    retry: false,
  });
}

/** 非链接入口复用相同解析结果，避免列表和编辑器另外裁剪路径。 */
export function FileReferenceText({
  path,
  className,
}: {
  path: string;
  className?: string;
}) {
  const reference = parseReference(path, "path");
  if (reference?.kind !== "file")
    return <span className={className}>{path}</span>;
  return <ResolvedReferenceText reference={reference} className={className} />;
}

function ResolvedReferenceText({
  reference,
  className,
}: {
  reference: FileReferenceCandidate;
  className?: string;
}) {
  const query = useFileReference(reference);
  const file =
    query.data?.status === "resolved"
      ? query.data.file
      : query.data?.status === "directory"
        ? query.data.directory
        : undefined;
  return (
    <span className={className} title={file?.canonicalPath ?? reference.raw}>
      {file?.displayPath ?? reference.raw}
    </span>
  );
}

/** 消息与文档共享的引用呈现；语法入口只传引用，不再自行判断路径所属关系。 */
export function ReferenceLink({
  reference,
  children,
  className,
  fallback,
}: {
  reference: Reference;
  children?: ReactNode;
  className?: string;
  fallback?: ReactNode;
}) {
  const navigation = useContext(ReferenceContext);
  if (reference.kind === "file")
    return (
      <LocalReferenceLink
        reference={reference}
        className={className}
        fallback={fallback}
      >
        {children}
      </LocalReferenceLink>
    );
  return (
    <a
      href={reference.url}
      title={reference.url}
      data-chat-link="web"
      className={cn(
        "text-link decoration-link/50 hover:decoration-link focus-visible:ring-ring rounded-xs wrap-anywhere underline underline-offset-4 transition-colors outline-none focus-visible:ring-2",
        className,
      )}
      onClick={(event) => {
        event.preventDefault();
        if (navigation.onOpenURL) navigation.onOpenURL(reference.url);
        else void openExternalURL(reference.url);
      }}
    >
      <Link2
        aria-hidden="true"
        className="size-icon-sm mr-1 inline-block align-middle"
      />
      {children || reference.url}
    </a>
  );
}

function LocalReferenceLink({
  reference,
  children,
  className,
  fallback,
}: {
  reference: FileReferenceCandidate;
  children?: ReactNode;
  className?: string;
  fallback?: ReactNode;
}) {
  const { t } = useTranslation("chat");
  const { onOpenFile } = useContext(ReferenceContext);
  const [copiedPath, setCopiedPath] = useState("");
  const query = useFileReference(reference);
  const result = query.isError ? undefined : query.data;
  const file = result?.status === "resolved" ? result.file : undefined;
  const directory =
    result?.status === "directory" ? result.directory : undefined;
  const target = file ?? directory;
  // 候选在确认前始终保留原文；无法定位不是消息中的一张失败文件卡片。
  if (!target)
    return (
      fallback ?? <span className={className}>{children || reference.raw}</span>
    );
  const label = fileReferenceLabel(target, textOf(children), reference);
  const copied = copiedPath === target.canonicalPath;
  const copyLabel = t(copied ? "filePathCopied" : "fileCopyPath");
  const chipClassName = cn(
    "min-h-control-sm border-border bg-muted text-foreground inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-sm border px-2 py-0.5 align-middle font-mono no-underline",
    className,
  );
  const content = (
    <>
      <WorkspaceFileTypeIcon
        name={target.name}
        isDirectory={Boolean(directory)}
        size={14}
      />
      <span className="min-w-0 break-all">{label}</span>
      {file?.line ? (
        <span className="type-caption shrink-0">
          :{file.line}
          {file.endLine
            ? `–${file.endLine}`
            : file.column
              ? `:${file.column}`
              : ""}
        </span>
      ) : null}
    </>
  );
  return (
    <span className="inline-flex max-w-full items-center gap-0.5 align-middle">
      {file ? (
        <a
          href={localFileURL(file.canonicalPath)}
          tabIndex={onOpenFile ? 0 : -1}
          aria-disabled={!onOpenFile}
          title={file.canonicalPath}
          data-chat-link="file"
          data-file-path={file.canonicalPath}
          className={cn(
            chipClassName,
            "hover:border-border-strong hover:bg-canvas focus-visible:ring-ring transition-colors outline-none focus-visible:ring-2",
          )}
          onClick={(event) => {
            event.preventDefault();
            onOpenFile?.(file);
          }}
        >
          {content}
        </a>
      ) : (
        <span
          title={target.canonicalPath}
          data-chat-link="directory"
          data-directory-path={target.canonicalPath}
          className={chipClassName}
        >
          {content}
        </span>
      )}
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="shrink-0"
        aria-label={`${copyLabel}：${target.name}`}
        title={copyLabel}
        onClick={async (event) => {
          event.preventDefault();
          event.stopPropagation();
          try {
            await navigator.clipboard.writeText(target.canonicalPath);
            setCopiedPath(target.canonicalPath);
          } catch {
            toast.error(t("filePathCopyFailed"));
          }
        }}
      >
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      </Button>
    </span>
  );
}
