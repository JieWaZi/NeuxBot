import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy, Link2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/sonner";
import { openExternalURL, revealLocalPath } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { cn } from "@/shared/lib/cn";
import { WorkspaceFileTypeIcon } from "./workspace-file-type-icon";
import { workspaceFileAppearance } from "./workspace-file-icons";
import {
  emptyFileReferenceContext,
  fileName,
  isAbsoluteFilePath,
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
  allowLocalReveal?: boolean;
};
const ReferenceContext = createContext<ReferenceNavigation>({
  context: emptyFileReferenceContext,
});

export function FileReferenceProvider({
  children,
  context,
  onOpenFile,
  onOpenURL,
  allowLocalReveal,
}: ReferenceNavigation & { children: ReactNode }) {
  const navigation = useMemo(
    () => ({ context, onOpenFile, onOpenURL, allowLocalReveal }),
    [context, onOpenFile, onOpenURL, allowLocalReveal],
  );
  return (
    <ReferenceContext.Provider value={navigation}>
      {children}
    </ReferenceContext.Provider>
  );
}

/** 读取既有授权上下文，不触发文件存在性请求。 */
export function useFileReferenceContext() {
  return useContext(ReferenceContext).context;
}

function isPlainPrimaryActivation(
  event: MouseEvent<HTMLAnchorElement>,
): boolean {
  return (
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey
  );
}

export function useFileReference(
  reference: FileReferenceCandidate | undefined,
  scope?: FileReferenceContext,
  active = true,
) {
  const navigation = useContext(ReferenceContext);
  const context = scope ?? navigation.context;
  return useQuery({
    queryKey: ["file-reference", context, reference?.path],
    queryFn: () =>
      reference
        ? resolveFileReference(
            {
              ...reference,
              line: undefined,
              column: undefined,
              endLine: undefined,
            },
            context,
          )
        : Promise.resolve({ status: "unavailable" as const }),
    select: (result) =>
      result.status === "resolved"
        ? {
            ...result,
            file: {
              ...result.file,
              line: reference?.line,
              column: reference?.column,
              endLine: reference?.endLine,
            },
          }
        : result,
    enabled: active && Boolean(reference && context.workspaceRoot),
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
  return (
    <span className={className} title={reference.raw}>
      {reference.raw}
    </span>
  );
}

/** 网站图标不读取页面元数据；失败保留随主题继承颜色的链接图标。 */
function WebsiteLinkIcon({ url }: { url: string }) {
  const [status, setStatus] = useState<"pending" | "loaded" | "failed">(
    "pending",
  );
  let iconURL: string | undefined;
  try {
    const target = new URL(url);
    if (target.protocol === "https:" || target.protocol === "http:")
      iconURL = `${target.origin}/favicon.ico`;
  } catch {
    // 无效目标沿用链接图标，不发起图标请求。
  }
  return (
    <span
      aria-hidden="true"
      className="size-icon-inline relative mr-1 inline-block align-middle"
    >
      {status !== "loaded" && <Link2 className="size-full" />}
      {iconURL && status !== "failed" && (
        <img
          src={iconURL}
          alt=""
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          className={cn(
            "absolute inset-0 size-full rounded-xs object-contain",
            status === "loaded"
              ? "bg-surface-panel dark:bg-content-primary"
              : "opacity-0",
          )}
          onLoad={() => setStatus("loaded")}
          onError={() => setStatus("failed")}
        />
      )}
    </span>
  );
}

/** 消息与文档共享的引用呈现；语法入口只传引用，不再自行判断路径所属关系。 */
export function ReferenceLink({
  reference,
  children,
  className,
  title,
  showCopy = true,
}: {
  reference: Reference;
  children?: ReactNode;
  className?: string;
  title?: string;
  showCopy?: boolean;
}) {
  const navigation = useContext(ReferenceContext);
  if (reference.kind === "file")
    return (
      <LocalReferenceLink
        reference={reference}
        className={className}
        title={title}
        showCopy={showCopy}
      >
        {children}
      </LocalReferenceLink>
    );
  return (
    <a
      href={reference.url}
      title={title ?? reference.url}
      target="_blank"
      rel="noopener noreferrer"
      data-chat-link="web"
      className={cn(
        "text-link decoration-link/50 hover:decoration-link focus-visible:ring-ring rounded-xs font-semibold wrap-anywhere underline underline-offset-4 transition-colors outline-none focus-visible:ring-2",
        className,
      )}
      onClick={(event) => {
        if (event.defaultPrevented || !isPlainPrimaryActivation(event)) return;
        event.preventDefault();
        if (navigation.onOpenURL) navigation.onOpenURL(reference.url);
        else void openExternalURL(reference.url);
      }}
    >
      <WebsiteLinkIcon key={reference.url} url={reference.url} />
      {children || reference.url}
    </a>
  );
}

function LocalReferenceLink({
  reference,
  children,
  className,
  title,
  showCopy,
}: {
  reference: FileReferenceCandidate;
  children?: ReactNode;
  className?: string;
  title?: string;
  showCopy: boolean;
}) {
  const { t } = useTranslation("chat");
  const { onOpenFile, allowLocalReveal } = useContext(ReferenceContext);
  const [copyFeedback, setCopyFeedback] = useState<{ path: string }>();
  useEffect(() => {
    if (!copyFeedback) return;
    const timer = window.setTimeout(() => setCopyFeedback(undefined), 5000);
    return () => window.clearTimeout(timer);
  }, [copyFeedback]);
  const query = useFileReference(reference, undefined, false);
  const result = query.isError ? undefined : query.data;
  const file = result?.status === "resolved" ? result.file : undefined;
  const directory =
    result?.status === "directory" ? result.directory : undefined;
  const target = file ?? directory;
  const name = target?.name ?? fileName(reference.path);
  const path = target?.canonicalPath ?? reference.path;
  const label = children ?? reference.raw;
  const copied = copyFeedback?.path === path;
  const copyLabel = t(copied ? "filePathCopied" : "fileCopyPath");
  const { tone } = workspaceFileAppearance(name, Boolean(directory));
  const chipClassName = cn(
    "file-reference min-h-control-sm inline-flex max-w-full min-w-0 items-center gap-1 rounded-sm px-1 py-0.5 align-middle no-underline",
    className,
  );
  const content = (
    <>
      <WorkspaceFileTypeIcon
        name={name}
        isDirectory={Boolean(directory)}
        size="var(--spacing-icon-inline)"
        className="text-current"
      />
      <span className="min-w-0 truncate">{label}</span>
    </>
  );
  // 工作台沿用授权解析。显式点击时，桌面可在系统文件管理器定位绝对路径。
  return (
    <span className="inline-flex max-w-full items-center gap-0.5 align-middle">
      {!directory ? (
        <a
          href={localFileURL(path)}
          tabIndex={onOpenFile ? 0 : -1}
          aria-disabled={!onOpenFile}
          title={title ?? path}
          data-chat-link="file"
          data-file-tone={tone}
          data-file-path={path}
          className={cn(
            chipClassName,
            "focus-visible:ring-ring transition-colors outline-none focus-visible:ring-2",
          )}
          onClick={async (event) => {
            if (event.defaultPrevented) return;
            const activate = isPlainPrimaryActivation(event);
            event.preventDefault();
            if (!activate || !onOpenFile) return;
            const resolved = file
              ? { status: "resolved" as const, file }
              : (await query.refetch()).data;
            if (resolved?.status === "resolved") {
              onOpenFile(resolved.file);
              return;
            }
            if (resolved?.status === "directory") return;
            if (
              allowLocalReveal &&
              resolved?.status !== "ambiguous" &&
              isAbsoluteFilePath(reference.path) &&
              (await revealLocalPath(reference.path).catch(() => false))
            )
              return;
            toast.error(
              t(`fileReference.${resolved?.status ?? "unavailable"}`),
            );
          }}
          onAuxClick={(event) => event.preventDefault()}
        >
          {content}
        </a>
      ) : (
        <span
          title={title ?? path}
          data-chat-link="directory"
          data-file-tone={tone}
          data-directory-path={path}
          className={chipClassName}
        >
          {content}
        </span>
      )}
      {showCopy && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          aria-label={`${copyLabel}：${name}`}
          title={copyLabel}
          onClick={async (event) => {
            event.preventDefault();
            event.stopPropagation();
            try {
              const resolved = target
                ? undefined
                : (await query.refetch()).data;
              const fullPath =
                target?.canonicalPath ??
                (resolved?.status === "resolved"
                  ? resolved.file.canonicalPath
                  : resolved?.status === "directory"
                    ? resolved.directory.canonicalPath
                    : reference.path);
              await navigator.clipboard.writeText(fullPath);
              setCopyFeedback({ path: fullPath });
            } catch {
              toast.error(t("filePathCopyFailed"));
            }
          }}
        >
          {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
        </Button>
      )}
    </span>
  );
}
