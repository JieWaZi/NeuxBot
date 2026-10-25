import { StreamdownTextPrimitive } from "@assistant-ui/react-streamdown";
import { code } from "@streamdown/code";
import { mermaid } from "./chat-mermaid-plugin";
import {
  CodeBlock,
  CodeBlockCopyButton,
  defaultRemarkPlugins,
  type StreamdownProps,
  useIsCodeFenceIncomplete,
} from "streamdown";
import {
  createContext,
  isValidElement,
  useContext,
  useMemo,
  type ComponentPropsWithoutRef,
  type HTMLAttributes,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";

import { Button } from "@/shared/ui/button";

import { ChatMermaid } from "./chat-mermaid";
import { ChatTable } from "./chat-table";
import { parseReference } from "@/shared/workspace/file-reference";
import { ReferenceLink } from "@/shared/workspace/file-reference-link";
import { remarkWorkspaceLinks } from "../model/chat-link";

const chatStreamdownPlugins = {
  code,
  mermaid,
};

const chatCodeLanguageAliases: Record<string, string> = {
  htm: "html",
};

const InsideChatLinkContext = createContext(false);

type ChatCodeProps = ComponentPropsWithoutRef<"code"> & {
  node?: unknown;
  "data-block"?: string;
};

function extractCode(children: unknown): string {
  if (typeof children === "string") return children;
  if (!isValidElement(children)) return "";
  const props = children.props as { children?: unknown } | null;
  return props && typeof props.children === "string" ? props.children : "";
}

function ChatCode({
  children,
  className,
  node: _node,
  "data-block": dataBlock,
  renderMermaid = true,
  allowFileReferences = true,
  ...props
}: ChatCodeProps & { renderMermaid?: boolean; allowFileReferences?: boolean }) {
  const { t } = useTranslation("chat");
  void _node;
  const isIncomplete = useIsCodeFenceIncomplete();
  const insideLink = useContext(InsideChatLinkContext);
  if (!dataBlock) {
    const text = typeof children === "string" ? children.trim() : "";
    const reference =
      allowFileReferences && !insideLink
        ? parseReference(text, "inline")
        : null;
    const code = (
      <code
        className={cn(
          "aui-streamdown-inline-code font-mono",
          !insideLink &&
            "border-border/60 bg-muted rounded-xs border px-1 py-0.5",
          className,
        )}
        {...props}
      >
        {children}
      </code>
    );
    return reference ? (
      <ReferenceLink
        reference={reference}
        className="font-mono"
        fallback={code}
      />
    ) : (
      code
    );
  }

  const fenceLanguage = className?.match(/language-([^\s]+)/)?.[1] ?? "text";
  const language =
    chatCodeLanguageAliases[fenceLanguage.toLowerCase()] ?? fenceLanguage;
  const codeText = extractCode(children);
  if (language === "mermaid" && renderMermaid) {
    return <ChatMermaid code={codeText} />;
  }
  return (
    <CodeBlock
      code={codeText}
      language={language}
      className={className}
      isIncomplete={isIncomplete}
    >
      <Button asChild size="icon-sm" variant="ghost">
        <CodeBlockCopyButton aria-label={t("codeCopy")} title={t("codeCopy")} />
      </Button>
    </CodeBlock>
  );
}

type ChatLinkProps = ComponentPropsWithoutRef<"a"> & {
  node?: unknown;
};

function ChatLink({
  children,
  className,
  href = "",
  node: _node,
  allowFileReferences = true,
}: ChatLinkProps & { allowFileReferences?: boolean }) {
  void _node;
  if (!allowFileReferences) {
    if (!/^https?:\/\//i.test(href))
      return <span className={className}>{children}</span>;
    return (
      <ReferenceLink
        reference={{ kind: "web", url: href }}
        className={className}
      >
        {children}
      </ReferenceLink>
    );
  }
  const reference = parseReference(href, "link");
  if (!reference) return <span className={className}>{children}</span>;
  return (
    <InsideChatLinkContext.Provider value={true}>
      <ReferenceLink reference={reference} className={className}>
        {children}
      </ReferenceLink>
    </InsideChatLinkContext.Provider>
  );
}

const userMarkdownComponents = { a: ChatLink, code: ChatCode };
const assistantMarkdownControls = { code: true, mermaid: true, table: false };

const chatLinkSafety = { enabled: false } as const;

// 完整流式快照通过字符队列平滑追赶目标文本。
// 直接使用 assistant-ui 提供的能力，避免在业务组件中维护第二套动画状态机。
const chatSmoothStream = {
  drainMs: 180,
  maxCharIntervalMs: 10,
  maxCharsPerFrame: 8,
  minCommitMs: 16,
} as const;

const assistantMarkdownContainerProps = {
  "data-chat-markdown": "assistant",
} as HTMLAttributes<HTMLDivElement>;

const userMarkdownContainerProps = {
  "data-chat-markdown": "user",
} as HTMLAttributes<HTMLDivElement>;

const markdownClassName =
  "chat-markdown type-chat min-w-0 max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0 [&_p]:my-2 [&_p]:max-w-4xl [&_li]:max-w-4xl [&_ul]:my-2 [&_ol]:my-2 [&_pre]:type-chat-code [&_pre]:my-3 [&_pre]:max-w-full [&_code]:type-chat-code [&_[data-streamdown=code-block-actions]]:border-0 [&_[data-streamdown=code-block-actions]]:bg-transparent [&_[data-streamdown=code-block-actions]]:p-0 [&_[data-streamdown=code-block-actions]]:backdrop-blur-none [&_h1]:type-chat-title [&_h1]:mt-6 [&_h1]:mb-3 [&_h2]:type-chat-heading [&_h2]:mt-5 [&_h2]:mb-2 [&_h3]:type-chat-subheading [&_h3]:mt-4 [&_h3]:mb-2 [&_hr]:my-4 [&_hr]:border-0 [&_hr]:border-t [&_hr]:border-border/60 [&_blockquote]:my-4 [&_blockquote]:max-w-4xl [&_blockquote]:border-l-2 [&_blockquote]:border-border-strong [&_blockquote]:pl-4 [&_blockquote]:text-muted-foreground";

const workspaceRemarkPlugins: StreamdownProps["remarkPlugins"] = [
  ...Object.values(defaultRemarkPlugins),
  remarkWorkspaceLinks,
];

export function AssistantText({
  isTerminal = true,
  renderImage,
  allowFileReferences = true,
}: {
  isTerminal?: boolean;
  renderImage?: (props: ComponentPropsWithoutRef<"img">) => ReactNode;
  allowFileReferences?: boolean;
}) {
  const components = useMemo(
    () => ({
      a: (props: ChatLinkProps) => (
        <ChatLink {...props} allowFileReferences={allowFileReferences} />
      ),
      code: (props: ChatCodeProps) => (
        <ChatCode
          {...props}
          renderMermaid={isTerminal}
          allowFileReferences={allowFileReferences}
        />
      ),
      table: ChatTable,
      ...(renderImage ? { img: renderImage } : {}),
    }),
    [isTerminal, renderImage, allowFileReferences],
  );

  return (
    <div data-work-result-body={isTerminal || undefined}>
      <StreamdownTextPrimitive
        className={markdownClassName}
        components={components}
        controls={assistantMarkdownControls}
        defer={!isTerminal}
        linkSafety={chatLinkSafety}
        mode={isTerminal ? "static" : "streaming"}
        plugins={chatStreamdownPlugins}
        remarkPlugins={workspaceRemarkPlugins}
        smooth={isTerminal ? false : chatSmoothStream}
        containerProps={assistantMarkdownContainerProps}
      />
    </div>
  );
}

export function UserText() {
  return (
    <StreamdownTextPrimitive
      className={`${markdownClassName} [&_code]:text-inherit [&_pre]:max-w-full [&_pre]:overflow-x-auto`}
      components={userMarkdownComponents}
      controls={false}
      linkSafety={chatLinkSafety}
      mode="static"
      plugins={chatStreamdownPlugins}
      remarkPlugins={workspaceRemarkPlugins}
      smooth={false}
      containerProps={userMarkdownContainerProps}
    />
  );
}
