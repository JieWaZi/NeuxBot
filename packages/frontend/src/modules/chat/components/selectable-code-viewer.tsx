import { useEffect, useMemo, useState } from "react";
import type { BundledLanguage } from "shiki";

import { cn } from "@/shared/lib/cn";

const MAX_HIGHLIGHT_CHARS = 200_000;
const MAX_HIGHLIGHT_LINES = 5_000;
const CACHE_LIMIT = 24;

type CodeToken = { color?: string; content: string; fontStyle?: number };
type HighlightedCode = CodeToken[][];
type Theme = "dark" | "light";

const highlightCache = new Map<string, HighlightedCode>();

export type SelectableCodeViewerProps = {
  content: string;
  fileName: string;
  highlightLines?: { startLine: number; endLine: number };
  theme: Theme;
  wrap: boolean;
};

/**
 * Light DOM code preview. Keeping every source line in the document makes
 * native text selection reliable and keeps line references inspectable.
 */
export function SelectableCodeViewer({
  content,
  fileName,
  highlightLines,
  theme,
  wrap,
}: SelectableCodeViewerProps) {
  const plainLines = useMemo(() => toPlainTokens(content), [content]);
  const language = languageFromFileName(fileName);
  const highlightKey = `${theme}:${language}:${hashContent(content)}`;
  const [highlighted, setHighlighted] = useState<{
    key: string;
    lines: HighlightedCode;
  }>();
  const lines =
    highlighted?.key === highlightKey
      ? highlighted.lines
      : (highlightCache.get(highlightKey) ?? plainLines);

  useEffect(() => {
    if (import.meta.env.MODE === "test") return;
    if (!shouldHighlightCode(content)) return;
    if (language === "text") return;
    if (highlightCache.has(highlightKey)) return;

    let cancelled = false;
    const schedule =
      window.requestIdleCallback ??
      ((callback) => window.setTimeout(callback, 0));
    const cancel = window.cancelIdleCallback ?? window.clearTimeout;
    const task = schedule(() => {
      void import("shiki")
        .then(({ codeToTokens }) =>
          codeToTokens(content, {
            lang: language as BundledLanguage,
            theme: theme === "dark" ? "one-dark-pro" : "one-light",
          }),
        )
        .then((result) => {
          if (cancelled) return;
          const highlighted = result.tokens as HighlightedCode;
          rememberHighlight(highlightKey, highlighted);
          setHighlighted({ key: highlightKey, lines: highlighted });
        })
        .catch(() => {
          // Plain text is already visible; highlighting failure is non-fatal.
        });
    });
    return () => {
      cancelled = true;
      cancel(task);
    };
  }, [content, highlightKey, language, theme]);

  useEffect(() => {
    if (!highlightLines) return;
    const frame = window.requestAnimationFrame(() => {
      document
        .querySelector<HTMLElement>(
          `[data-selectable-code-viewer][data-file-name="${CSS.escape(fileName)}"] [data-line="${highlightLines.startLine}"]`,
        )
        ?.scrollIntoView({ block: "center" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [fileName, highlightLines]);

  return (
    <pre
      data-file-name={fileName}
      data-selectable-code-viewer
      data-wrap={wrap ? "wrap" : "scroll"}
      className={cn(
        "text-foreground type-body min-h-full min-w-max py-3 font-mono",
        wrap && "min-w-0 break-words whitespace-pre-wrap",
      )}
    >
      <code>
        {lines.map((tokens, index) => {
          const lineNumber = index + 1;
          const selected =
            highlightLines &&
            lineNumber >= highlightLines.startLine &&
            lineNumber <= highlightLines.endLine;
          return (
            <span
              key={lineNumber}
              data-line={lineNumber}
              className={cn(
                "grid min-h-5 grid-cols-[3.5rem_1fr] px-3",
                selected && "bg-primary/12",
              )}
            >
              <span
                aria-hidden="true"
                className="text-muted-foreground pointer-events-none pr-4 text-right tabular-nums select-none"
              >
                {lineNumber}
              </span>
              <span data-code-line className="min-w-0">
                {tokens.map((token, tokenIndex) => (
                  <span
                    key={tokenIndex}
                    style={{
                      color: token.color,
                      fontStyle: token.fontStyle === 1 ? "italic" : undefined,
                      fontWeight: token.fontStyle === 2 ? 700 : undefined,
                      textDecoration:
                        token.fontStyle === 4 ? "underline" : undefined,
                    }}
                  >
                    {token.content}
                  </span>
                ))}
                {index < lines.length - 1 ? "\n" : null}
              </span>
            </span>
          );
        })}
      </code>
    </pre>
  );
}

export function shouldHighlightCode(content: string): boolean {
  return (
    content.length <= MAX_HIGHLIGHT_CHARS &&
    countLinesUpTo(content, MAX_HIGHLIGHT_LINES + 1) <= MAX_HIGHLIGHT_LINES
  );
}

function toPlainTokens(content: string): HighlightedCode {
  return content.split("\n").map((line) => [{ content: line }]);
}

function countLinesUpTo(content: string, limit: number): number {
  let lines = 1;
  for (let index = 0; index < content.length && lines <= limit; index += 1) {
    if (content.charCodeAt(index) === 10) lines += 1;
  }
  return lines;
}

function languageFromFileName(fileName: string): string {
  const baseName = fileName.split("/").at(-1)?.toLowerCase() ?? "";
  if (baseName === "dockerfile") return "dockerfile";
  if (baseName === "makefile") return "makefile";
  const extension = baseName.split(".").at(-1) ?? "";
  return (
    (
      {
        c: "c",
        cc: "cpp",
        cpp: "cpp",
        cs: "csharp",
        css: "css",
        go: "go",
        h: "c",
        hpp: "cpp",
        html: "html",
        java: "java",
        js: "javascript",
        jsx: "jsx",
        json: "json",
        jsonc: "jsonc",
        kt: "kotlin",
        less: "less",
        lua: "lua",
        py: "python",
        rb: "ruby",
        rs: "rust",
        scss: "scss",
        sh: "bash",
        sql: "sql",
        svelte: "svelte",
        toml: "toml",
        ts: "typescript",
        tsx: "tsx",
        vue: "vue",
        xml: "xml",
        yaml: "yaml",
        yml: "yaml",
        zsh: "bash",
      } as Record<string, string>
    )[extension] ?? "text"
  );
}

function hashContent(content: string): string {
  let hash = 2166136261;
  for (let index = 0; index < content.length; index += 1) {
    hash ^= content.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `${content.length}:${hash >>> 0}`;
}

function rememberHighlight(key: string, lines: HighlightedCode): void {
  if (highlightCache.size >= CACHE_LIMIT) {
    const oldest = highlightCache.keys().next().value;
    if (oldest) highlightCache.delete(oldest);
  }
  highlightCache.set(key, lines);
}
