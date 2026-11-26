import { useQuery } from "@tanstack/react-query";
import {
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentType,
  type ComponentPropsWithoutRef,
  type ReactNode,
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

export type WorkspaceImagePresentationProps = {
  children: ReactNode;
  original: ReactNode;
  label: string;
};

type LocalImagePresentation = {
  fallback?: ReactNode;
  presentation?: ComponentType<WorkspaceImagePresentationProps>;
};

export type WorkspaceMarkdownImageProps = ComponentPropsWithoutRef<"img"> &
  LocalImagePresentation & {
    context?: FileReferenceContext;
    frameClassName?: string;
    /** 工作区预览显式沿用外链图片；聊天默认不请求远程地址。 */
    allowRemote?: boolean;
    node?: unknown;
  };

type ImageFailure = "failed" | "tooLarge" | "unsupported" | "remoteBlocked";
const previewMarker =
  /^https:\/\/bot\.invalid(?::443)?\/__workbench_markdown_image__(?:[?#]|$)/i;

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
        marker.origin === "https://bot.invalid" &&
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
  fallback,
  decorate,
  loading = "lazy",
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> & {
  src: string | Blob;
  fallback?: ReactNode;
  decorate?: (image: ReactNode) => ReactNode;
}) {
  const { t } = useTranslation("common");
  const [failedSource, setFailedSource] = useState<string | Blob>();
  const failed = failedSource === src;
  const decorated = Boolean(decorate);
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
  }, [src, failed, decorated]);
  if (failed)
    return (
      fallback ?? (
        <ImageFallback
          label={alt || t("markdownImage.label")}
          failure="failed"
        />
      )
    );
  const preview = (
    <img
      {...props}
      ref={image}
      alt={alt ?? ""}
      loading={loading}
      onError={(event) => {
        setFailedSource(src);
        props.onError?.(event);
      }}
    />
  );
  return decorate ? decorate(preview) : preview;
}

function ResourceImage({
  chatId,
  file,
  fallback,
  presentation: Presentation,
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> &
  LocalImagePresentation & {
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
  const source = query.data;
  if (source && !query.isError) {
    const label = props.alt || file.name;
    return (
      <BrowserImage
        {...props}
        src={source}
        fallback={fallback}
        decorate={
          Presentation
            ? (image) => (
                <Presentation
                  label={label}
                  original={
                    <BrowserImage src={source} alt={label} loading="eager" />
                  }
                >
                  {image}
                </Presentation>
              )
            : undefined
        }
      />
    );
  }
  if (fallback != null) return fallback;
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
  fallback,
  ...props
}: Omit<ComponentPropsWithoutRef<"img">, "src"> &
  LocalImagePresentation & {
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
        fallback={fallback}
        chatId={context.chatId}
        file={result.file}
      />
    );
  if (fallback != null) return fallback;
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
  frameClassName,
  fallback,
  presentation,
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
    <span
      ref={element}
      className={cn("inline-block max-w-full align-middle", frameClassName)}
    >
      {!active || !activated ? (
        (fallback ?? <ImageFallback label={label} />)
      ) : reference ? (
        context.chatId && context.workspaceRoot ? (
          <LocalImage
            {...props}
            className={cn("max-w-full", className)}
            alt={alt ?? ""}
            context={context}
            reference={reference}
            fallback={fallback}
            presentation={presentation}
          />
        ) : (
          (fallback ?? <ImageFallback label={label} failure="failed" />)
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
