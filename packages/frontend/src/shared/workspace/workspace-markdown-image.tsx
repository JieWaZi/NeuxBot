import { useQuery } from "@tanstack/react-query";
import {
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentPropsWithoutRef,
} from "react";
import { useTranslation } from "react-i18next";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { observeViewportActivation } from "@/shared/lib/viewport-activation";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useRouteSceneActive } from "./app-scene-workspace";
import {
  parseReference,
  type FileReferenceCandidate,
  type FileReferenceContext,
  type ResolvedFileReference,
} from "./file-reference";
import {
  useFileReference,
  useFileReferenceContext,
} from "./file-reference-link";
import {
  WorkspaceImageError,
  workspaceDataImageOptions,
  workspaceImageOptions,
} from "./workspace-image-query";

export type WorkspaceMarkdownImageProps = ComponentPropsWithoutRef<"img"> & {
  context?: FileReferenceContext;
  /** 工作区预览显式沿用外链图片；聊天默认不请求远程地址。 */
  allowRemote?: boolean;
  node?: unknown;
};

type ImageFailure = "failed" | "tooLarge" | "unsupported" | "remoteBlocked";
const previewMarker =
  /^https:\/\/ally\.invalid(?::443)?\/__workbench_markdown_image__(?:[?#]|$)/i;

function subscribeVisibility(update: () => void) {
  document.addEventListener("visibilitychange", update);
  return () => document.removeEventListener("visibilitychange", update);
}

function imageReference(src: string): FileReferenceCandidate | undefined {
  if (src.startsWith("//")) return undefined;
  let path = src;
  // 兼容已有工作区预览标记；标记本身绝不作为远程 URL 发出。
  if (previewMarker.test(src))
    try {
      const marker = new URL(src);
      if (
        marker.origin === "https://ally.invalid" &&
        marker.pathname === "/__workbench_markdown_image__"
      )
        path = marker.searchParams.get("path") ?? "";
    } catch {
      // 相对文件路径无需构造 URL。
    }
  const reference =
    parseReference(path, "link") ?? parseReference(path, "path");
  return reference?.kind === "file" ? reference : undefined;
}

function ImageFallback({
  label,
  failure,
  loading,
  onRetry,
}: {
  label: string;
  failure?: ImageFailure;
  loading?: boolean;
  onRetry?: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <span className="text-muted-foreground inline-flex max-w-full flex-wrap items-center gap-1.5 align-middle">
      <span
        role="img"
        aria-label={label || t("markdownImage.label")}
        className="wrap-anywhere"
      >
        {label || t("markdownImage.label")}
      </span>
      {failure ? (
        <span className="type-caption">{t(`markdownImage.${failure}`)}</span>
      ) : loading ? (
        <span
          role="status"
          aria-busy="true"
          className="bg-muted rounded-sm px-2"
        >
          {t("markdownImage.loading")}
        </span>
      ) : null}
      {onRetry ? (
        <Button type="button" variant="text" size="sm" onClick={onRetry}>
          {t("retry")}
        </Button>
      ) : null}
    </span>
  );
}

function BrowserImage({
  src,
  alt,
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> & { src: string | Blob }) {
  const { t } = useTranslation("common");
  const [failedSource, setFailedSource] = useState<string | Blob>();
  const failed = failedSource === src;
  const image = useRef<HTMLImageElement>(null);
  useEffect(() => {
    const element = image.current;
    if (failed || !element) return;
    const objectURL =
      typeof src === "string" ? undefined : URL.createObjectURL(src);
    element.src = objectURL ?? (typeof src === "string" ? src : "");
    return () => {
      element.removeAttribute("src");
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [src, failed]);
  return failed ? (
    <ImageFallback label={alt || t("markdownImage.label")} failure="failed" />
  ) : (
    <img
      {...props}
      ref={image}
      alt={alt ?? ""}
      loading="lazy"
      onError={(event) => {
        setFailedSource(src);
        props.onError?.(event);
      }}
    />
  );
}

function ResourceImage({
  chatId,
  file,
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> & {
  chatId: string;
  file: ResolvedFileReference;
}) {
  const query = useQuery(workspaceImageOptions(chatId, file));
  useWorkspaceQueryEvents(
    chatId,
    [file.rootPath],
    ["workspace-markdown-image"],
    true,
    file.path,
  );
  if (query.data && !query.isError)
    return <BrowserImage {...props} src={query.data} />;
  return (
    <ImageFallback
      label={props.alt || file.name}
      loading={query.isPending}
      failure={
        query.isError
          ? query.error instanceof WorkspaceImageError
            ? query.error.reason
            : "failed"
          : undefined
      }
      onRetry={query.isError ? () => void query.refetch() : undefined}
    />
  );
}

function DataImage({
  src,
  ...props
}: ComponentPropsWithoutRef<"img"> & { src: string }) {
  const query = useQuery(workspaceDataImageOptions(src));
  if (query.data) return <BrowserImage {...props} src={query.data} />;
  return (
    <ImageFallback
      label={props.alt ?? ""}
      loading={query.isPending}
      failure={
        query.isError
          ? query.error instanceof WorkspaceImageError
            ? query.error.reason
            : "failed"
          : undefined
      }
    />
  );
}

function LocalImage({
  context,
  reference,
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> & {
  context: FileReferenceContext;
  reference: FileReferenceCandidate;
}) {
  const query = useFileReference(reference, context);
  const result = query.data;
  if (result?.status === "resolved" && context.chatId)
    return (
      <ResourceImage
        key={`${context.chatId}:${result.file.canonicalPath}`}
        {...props}
        chatId={context.chatId}
        file={result.file}
      />
    );
  return (
    <ImageFallback
      label={props.alt || reference.raw}
      loading={query.isPending}
      failure={!query.isPending ? "failed" : undefined}
      onRetry={!query.isPending ? () => void query.refetch() : undefined}
    />
  );
}

/** 聊天与文档共用 Chat 授权读取；隐藏内容不持有传输、文件字节或 Blob URL。 */
export function WorkspaceMarkdownImage({
  context: explicitContext,
  allowRemote = false,
  src = "",
  alt,
  srcSet: _srcSet,
  node: _node,
  className,
  ...props
}: WorkspaceMarkdownImageProps) {
  void _srcSet;
  void _node;
  const inheritedContext = useFileReferenceContext();
  const context = explicitContext ?? inheritedContext;
  const contentActive = useContext(ContentVisibility);
  const sceneActive = useRouteSceneActive();
  const element = useRef<HTMLSpanElement>(null);
  const documentVisible = useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState === "visible",
    () => false,
  );
  const active = contentActive && sceneActive && documentVisible;
  const [activated, setActivated] = useState(false);
  useEffect(() => {
    if (!active || activated || !element.current) return;
    return observeViewportActivation(element.current, () => setActivated(true));
  }, [active, activated]);
  const { t } = useTranslation("common");
  const reference = imageReference(src);
  const label = alt || reference?.raw || t("markdownImage.label");
  const remote = !previewMarker.test(src) && /^(?:https?:)?\/\//i.test(src);
  const dataImage = /^data:/i.test(src);
  return (
    <span ref={element} className="inline-block max-w-full align-middle">
      {!active || !activated ? (
        <ImageFallback label={label} />
      ) : reference ? (
        context.chatId && context.workspaceRoot ? (
          <LocalImage
            {...props}
            className={cn("max-w-full", className)}
            alt={alt ?? ""}
            context={context}
            reference={reference}
          />
        ) : (
          <ImageFallback label={label} failure="failed" />
        )
      ) : dataImage && allowRemote ? (
        <DataImage
          key={src}
          {...props}
          className={cn("max-w-full", className)}
          src={src}
          alt={alt ?? ""}
        />
      ) : remote && allowRemote ? (
        <BrowserImage
          key={src}
          {...props}
          className={cn("max-w-full", className)}
          src={src}
          alt={alt ?? ""}
        />
      ) : (
        <ImageFallback
          label={label}
          failure={remote || dataImage ? "remoteBlocked" : "unsupported"}
        />
      )}
    </span>
  );
}
