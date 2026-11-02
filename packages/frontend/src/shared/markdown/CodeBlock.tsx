// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
import {
  Fragment,
  useMemo,
  useRef,
  useState,
  useEffect,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { Copy, Check } from "lucide-react";
import { Button } from "@/shared/ui/button";
import {
  StreamingHighlightSession,
  grammarLoadCount,
  highlightToHtml,
  subscribeGrammarLoaded,
  type HighlightSpan,
  type StreamingHighlightFrame,
} from "./highlight";
import { useViewportHighlighting } from "./useViewportHighlighting";
interface CodeBlockProps {
  code: string;
  lang?: string;
  streaming?: boolean;
  copyLabel: string;
  copiedLabel: string;
}
const SHIKI_PRE_PROPS = {
  className: "shiki css-variables",
  tabIndex: 0,
} as const;
const STREAMING_LINE_GROUP_SIZE = 32;

function renderLine(line: readonly HighlightSpan[], index: number): ReactNode {
  return (
    <Fragment key={index}>
      {index > 0 && "\n"}
      <span className="line">
        {line.map((span, spanIndex) => (
          <span key={spanIndex} style={span.style}>
            {span.text}
          </span>
        ))}
      </span>
    </Fragment>
  );
}

/** 每个代码块独占可丢弃的渲染缓存，保持完成行分组的 React 身份。 */
class StreamingCodeRenderer {
  private session: StreamingHighlightSession | null = null;
  private settled = false;
  private lineCache: {
    code: string;
    lang: string | undefined;
    generation: number;
    frame: StreamingHighlightFrame;
    groups: ReactNode[];
    pending: ReactNode[];
    nextLine: number;
    body: ReactNode;
  } | null = null;
  private streamedBody(
    code: string,
    lang: string | undefined,
    streaming: boolean,
    highlighting: boolean,
  ): ReactNode | undefined {
    const trimmed = code;
    if (!highlighting) {
      this.session = null;
      this.lineCache = null;
      this.settled = false;
      return undefined;
    }
    if (streaming !== true) {
      const previous = this.lineCache;
      if (
        previous !== null &&
        previous.code === trimmed &&
        previous.lang === lang
      ) {
        this.settled = true;
        return previous.body;
      }
      this.session = null;
      this.lineCache = null;
      this.settled = true;
      return undefined;
    }
    if (this.settled) {
      this.session = null;
      this.lineCache = null;
      this.settled = false;
    }
    this.session ??= new StreamingHighlightSession();
    const frame = this.session.updateFrame(trimmed, lang);
    if (frame === undefined) {
      this.lineCache = null;
      return undefined;
    }
    const previous = this.lineCache;
    if (
      previous?.frame === frame &&
      previous.code === trimmed &&
      previous.lang === lang
    ) {
      return previous.body;
    }
    const sameGeneration = previous?.generation === frame.generation;
    const groups = sameGeneration ? [...previous.groups] : [];
    let pending = sameGeneration ? [...previous.pending] : [];
    let nextLine = sameGeneration ? previous.nextLine : 0;
    for (const line of frame.appended) {
      pending.push(renderLine(line, nextLine));
      nextLine += 1;
      if (pending.length !== STREAMING_LINE_GROUP_SIZE) continue;
      const start = nextLine - pending.length;
      groups.push(<Fragment key={start}>{pending}</Fragment>);
      pending = [];
    }
    const tail = frame.tail.map((line, index) =>
      renderLine(line, nextLine + index),
    );
    const tailGroup = (
      <Fragment key={nextLine - pending.length}>
        {[...pending, ...tail]}
      </Fragment>
    );
    const body = (
      <pre {...SHIKI_PRE_PROPS}>
        <code>
          {groups}
          {tailGroup}
        </code>
      </pre>
    );
    this.lineCache = {
      code: trimmed,
      lang,
      generation: frame.generation,
      frame,
      groups,
      pending,
      nextLine,
      body,
    };
    return body;
  }

  render(
    code: string,
    lang: string | undefined,
    streaming: boolean,
    highlighting: boolean,
    grammarRevision: number,
  ) {
    // 语法注册版本由组件订阅推进；完成行缓存的协议仍由 session 核验。
    void grammarRevision;
    const streamedBody = this.streamedBody(code, lang, streaming, highlighting);
    const html =
      highlighting && !streaming && streamedBody === undefined
        ? highlightToHtml(code, lang)
        : undefined;
    return { streamedBody, html };
  }
}

export function CodeBlock({
  code,
  lang,
  streaming = false,
  copyLabel,
  copiedLabel,
}: CodeBlockProps) {
  const trimmed = code;
  const rootRef = useRef<HTMLDivElement>(null);
  const highlighting = useViewportHighlighting(rootRef, lang);
  const loaded = useSyncExternalStore(
    subscribeGrammarLoaded,
    grammarLoadCount,
    grammarLoadCount,
  );
  const renderer = useMemo(() => new StreamingCodeRenderer(), []);
  const { streamedBody, html } = useMemo(
    () => renderer.render(code, lang, streaming, highlighting, loaded),
    [renderer, code, lang, streaming, highlighting, loaded],
  );
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <section
      ref={rootRef}
      className="bg-muted my-3 min-w-0 overflow-hidden rounded-lg"
      data-streamdown="code-block"
    >
      <header className="text-muted-foreground type-caption flex items-center justify-between gap-3 px-4 py-1">
        <span>{lang ?? "text"}</span>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={copied ? copiedLabel : copyLabel}
          title={copied ? copiedLabel : copyLabel}
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(`${code}\n`);
              setCopied(true);
            } catch {
              setCopied(false);
            }
          }}
        >
          {copied ? <Check /> : <Copy />}
        </Button>
      </header>
      <div
        className="type-chat-code max-w-full overflow-x-auto px-4 pb-4 font-mono [&_code]:font-mono [&_pre]:m-0 [&_pre]:bg-transparent"
        data-code-block-content
        data-streamdown="code-block-body"
      >
        {streamedBody !== undefined ? (
          streamedBody
        ) : html === undefined ? (
          <pre tabIndex={0}>
            <code>{trimmed}</code>
          </pre>
        ) : (
          <div dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </div>
    </section>
  );
}
