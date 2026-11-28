import {
  Children,
  useMemo,
  useRef,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { Link, useInRouterContext } from "react-router-dom";
import { cn } from "@/shared/lib/cn";
import {
  MarkdownText,
  type MarkdownComponents,
  type MarkdownCodeProps,
  type MarkdownImageProps,
  type MarkdownInlineCodeProps,
  type MarkdownLinkProps,
} from "@/shared/markdown/MarkdownText";
import { CodeBlock } from "@/shared/markdown/CodeBlock";
import { ChatMermaid } from "./chat-mermaid";
import { ChatPreviewDialog } from "./chat-preview-dialog";
import {
  ChatPreviewViewport,
  useChatPreviewFit,
} from "./chat-preview-viewport";
import { ChatTable } from "./chat-table";
import { parseReference } from "@/shared/workspace/file-reference";
import { ReferenceLink } from "@/shared/workspace/file-reference-link";
import { isWorkspaceMarkdownImagePath } from "@/shared/workspace/workspace-image-query";
import {
  WorkspaceMarkdownImage,
  type WorkspaceImagePresentationProps,
} from "@/shared/workspace/workspace-markdown-image";

// 预览位置由消息确定，离开会话释放图片资源后仍保留相同的布局。
const thumbnailFrameClassName =
  "bg-muted/40 relative inline-grid aspect-video w-64 max-w-full place-items-center overflow-hidden rounded-lg";
const thumbnailClassName = "absolute inset-0 size-full object-contain";

function readImageSize(content: HTMLDivElement) {
  const image = content.querySelector("img");
  if (!image) return;
  return { width: image.naturalWidth, height: image.naturalHeight };
}

function ChatImageViewport({ children }: { children: ReactNode }) {
  const imageRef = useRef<HTMLDivElement>(null);
  const { fit, fittedSize, viewportRef } = useChatPreviewFit(
    imageRef,
    readImageSize,
    { fitHeight: true },
  );
  return (
    <ChatPreviewViewport viewportRef={viewportRef} fittedSize={fittedSize}>
      {() => (
        <div
          ref={imageRef}
          onLoadCapture={fit}
          className="[&_img]:block [&_img]:h-auto [&_img]:w-full"
        >
          {children}
        </div>
      )}
    </ChatPreviewViewport>
  );
}

function ChatImagePreview({
  children,
  original,
  label,
}: WorkspaceImagePresentationProps) {
  return (
    <ChatPreviewDialog
      title={label}
      trigger={
        <button
          type="button"
          aria-label={label}
          className="focus-visible:ring-ring absolute inset-0 z-0 cursor-zoom-in rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-inset"
        >
          {children}
        </button>
      }
    >
      <ChatImageViewport>{original}</ChatImageViewport>
    </ChatPreviewDialog>
  );
}

function ChatImage({ inLink, ...props }: MarkdownImageProps) {
  return (
    <WorkspaceMarkdownImage
      {...props}
      frameClassName={thumbnailFrameClassName}
      className={thumbnailClassName}
      fallback={inLink ? props.alt || props.src : undefined}
      presentation={inLink ? undefined : ChatImagePreview}
    />
  );
}

function ChatInlineCode({
  children,
  inLink,
  allowFileReferences,
}: MarkdownInlineCodeProps & { allowFileReferences: boolean }) {
  const candidate =
    allowFileReferences && !inLink
      ? parseReference(children.trim(), "inline")
      : null;
  // 行内代码不是文件事实；没有已确认的文件映射时保留原文，避免猜测路径或逐项读取。
  const reference = candidate?.kind === "web" ? candidate : null;
  const code = (
    <code
      className={cn(
        "font-mono",
        !inLink &&
          "border-border/60 bg-muted text-foreground rounded-xs border px-1 py-0.5",
      )}
    >
      {children}
    </code>
  );
  return reference ? (
    <ReferenceLink reference={reference} className="font-mono">
      {children}
    </ReferenceLink>
  ) : (
    code
  );
}
function ChatCode({ code, language, streaming }: MarkdownCodeProps) {
  const { t } = useTranslation(["chat", "plugin"]);
  if (language === "mermaid" && !streaming) return <ChatMermaid code={code} />;
  return (
    <CodeBlock
      code={code}
      lang={language}
      streaming={streaming}
      copyLabel={t("chat:codeCopy")}
      copiedLabel={t("plugin:codeCopied")}
    />
  );
}
function ChatLink({
  children,
  href = "",
  className,
  title,
  standalone,
  allowFileReferences,
  showImageLinks,
  allowAutomaticTaskLinks,
}: MarkdownLinkProps & {
  allowFileReferences: boolean;
  showImageLinks: boolean;
  allowAutomaticTaskLinks: boolean;
}) {
  const inRouter = useInRouterContext();
  if (
    inRouter &&
    allowAutomaticTaskLinks &&
    /^\/automatic-tasks\/[A-Za-z0-9_-]{1,32}$/.test(href)
  ) {
    return (
      <Link to={href} className={className} title={title}>
        {children}
      </Link>
    );
  }
  const reference = allowFileReferences
    ? parseReference(href, "link")
    : /^https?:\/\//i.test(href)
      ? { kind: "web" as const, url: href }
      : null;
  if (
    showImageLinks &&
    standalone &&
    reference?.kind === "file" &&
    reference.line === undefined &&
    !href.startsWith("//") &&
    isWorkspaceMarkdownImagePath(reference.path)
  ) {
    const [label] = Children.toArray(children);
    if (typeof label === "string")
      return (
        <WorkspaceMarkdownImage
          src={href}
          alt={label}
          title={title}
          frameClassName={thumbnailFrameClassName}
          className={thumbnailClassName}
          presentation={ChatImagePreview}
          fallback={
            <ReferenceLink
              reference={reference}
              className={className}
              title={title}
              showCopy={false}
            >
              {children}
            </ReferenceLink>
          }
        />
      );
  }
  return reference ? (
    <ReferenceLink reference={reference} className={className} title={title}>
      {children}
    </ReferenceLink>
  ) : (
    <span className={className}>{children}</span>
  );
}

const markdownClassName = cn(
  "chat-markdown type-chat min-w-0 max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0 [&_p]:my-4 [&_p]:max-w-4xl [&_p:first-child]:mt-0 [&_p:last-child]:mb-0",
  "[&_h1]:type-chat-title [&_h1]:mt-8 [&_h1]:mb-4 [&_h2]:type-chat-heading [&_h2]:mt-8 [&_h2]:mb-4 [&_h3]:type-chat-subheading [&_h3]:mt-8 [&_h3]:mb-4 [&_h4]:type-title [&_h4]:my-4 [&_h5]:type-chat [&_h5]:my-4 [&_h6]:type-chat [&_h6]:my-4 [&_:is(h1,h2,h3,h4,h5,h6)]:font-semibold [&_:is(h1,h2,h3,h4,h5,h6)_strong]:[font-weight:inherit] [&_:is(h4,h5,h6):has(+:is(ul,ol))]:mb-2 [&_:is(h4,h5,h6)+:is(ul,ol)]:mt-2",
  "[&_ul]:my-4 [&_ol]:my-4 [&_li]:my-0 [&_li]:max-w-4xl [&_li+li]:mt-1.5 [&_li>:is(ul,ol)]:mt-1 [&_li>p]:my-2 [&_li>*:first-child]:mt-0 [&_li>*:last-child:not([data-streamdown=code-block])]:mb-0 [&_:is(ul,ol)_ol]:list-inside [&_:is(ul,ol)_ol]:pl-0 [&_:is(ul,ol)_ol_li>p]:inline [&_li::marker]:text-muted-foreground",
  // 行内代码随正文或标题缩放；表格使用 type-body，缩放后仍不低于 caption。
  "[&_:not(pre)>code]:type-chat-inline-code [&_pre]:type-chat-code [&_pre]:my-4 [&_pre]:max-w-full [&_pre_code]:[font-size:inherit] [&_:is(h1,h2,h3,h4,h5,h6)_code]:[font-weight:inherit]",
  "[&_[data-streamdown=code-block-actions]]:border-0 [&_[data-streamdown=code-block-actions]]:bg-transparent [&_[data-streamdown=code-block-actions]]:p-0 [&_[data-streamdown=code-block-actions]]:backdrop-blur-none [&_[data-streamdown=table-wrapper]]:max-w-none [&_[data-streamdown=table-wrapper]]:w-full",
  "[&_p:has(+[data-streamdown=table-wrapper])]:mb-2 [&_[data-streamdown=table-wrapper]+p]:mt-2",
  "[&_hr]:my-8 [&_hr]:border-0 [&_hr]:border-t [&_hr]:border-border/60 [&_blockquote]:my-4 [&_blockquote]:max-w-4xl [&_blockquote]:border-l-2 [&_blockquote]:border-border-strong [&_blockquote]:pl-4 [&_blockquote]:text-muted-foreground",
);

export type AssistantMarkdownProps = {
  text: string;
  isTerminal?: boolean;
  renderImage?: (props: ComponentPropsWithoutRef<"img">) => ReactNode;
  allowFileReferences?: boolean;
};
/** 聊天正文直接消费唯一节点文本；不再排队逐字追赶服务器内容。 */
function MarkdownBody({
  text,
  isTerminal = true,
  renderImage: CustomImage,
  allowFileReferences = true,
  role,
}: AssistantMarkdownProps & { role: "assistant" | "user" }) {
  const { t } = useTranslation(["chat", "plugin"]);
  const labels = useMemo(
    () => ({
      code: {
        copyLabel: t("chat:codeCopy"),
        copiedLabel: t("plugin:codeCopied"),
      },
      footnotes: t("chat:markdownFootnotes"),
    }),
    [t],
  );
  const components = useMemo<MarkdownComponents>(
    () => ({
      link: (props) => (
        <ChatLink
          {...props}
          allowFileReferences={allowFileReferences}
          showImageLinks={isTerminal && !CustomImage}
          allowAutomaticTaskLinks={role === "assistant"}
        />
      ),
      inlineCode: (props) => (
        <ChatInlineCode {...props} allowFileReferences={allowFileReferences} />
      ),
      code: ChatCode,
      table: ChatTable,
      ...(CustomImage
        ? {
            image: ({ inLink, ...props }) => {
              void inLink;
              return <CustomImage {...props} />;
            },
          }
        : isTerminal && allowFileReferences
          ? { image: ChatImage }
          : {}),
    }),
    [allowFileReferences, CustomImage, isTerminal, role],
  );
  return (
    <MarkdownText
      text={text}
      streaming={!isTerminal}
      labels={labels}
      className={cn(
        markdownClassName,
        role === "user" &&
          "[&_[data-streamdown=code-block]]:bg-surface-tertiary [&_[data-streamdown=code-block]]:text-foreground [&_[data-streamdown=table-wrapper]]:bg-surface-panel [&_[data-streamdown=table-wrapper]]:text-foreground [&_a]:decoration-current [&_a[data-chat-link=web]]:text-inherit [&_pre]:max-w-full [&_pre]:overflow-x-auto",
      )}
      components={components}
      data-chat-markdown={role}
    />
  );
}
export function AssistantMarkdown(props: AssistantMarkdownProps) {
  return (
    <div data-chat-readable-body>
      <MarkdownBody {...props} role="assistant" />
    </div>
  );
}
export function UserMarkdown({ text }: { text: string }) {
  return <MarkdownBody text={text} role="user" />;
}
