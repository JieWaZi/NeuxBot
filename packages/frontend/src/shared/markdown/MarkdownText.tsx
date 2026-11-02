// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
/**
 * Untrusted assistant-Markdown renderer over the direct mdast pipeline:
 * `parse.ts` grammars, the incremental streaming parser, and `render.tsx`.
 * While a message streams, all but the trailing two blocks freeze as cached
 * React elements and only the source tail behind them re-parses per chunk,
 * so per-chunk work tracks the tail size instead of the whole reply. Frozen
 * blocks keep their source-offset keys when they cross the freeze boundary,
 * so React reconciles instead of remounting. Known deviation while
 * streaming: a reference-style link or footnote whose definition sits on the
 * other side of the freeze boundary renders literally until the settled
 * full parse self-heals it.
 */

import { memo, useMemo, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "@/shared/lib/cn";
import { IncrementalMarkdownParser } from "./incremental";
import { parseGfm, parseGfmWithMath } from "./parse";
import {
  collectReferenceTargets,
  createReferenceTargets,
  renderBlocks,
  renderFootnoteSection,
  wrapBlockChildren,
  emptyComponents,
} from "./render";
import type {
  MarkdownLabels,
  MarkdownComponents,
  MarkdownRenderContext,
  ReferenceTargets,
} from "./render";
import "katex/dist/katex.min.css";
export type {
  MarkdownComponents,
  MarkdownCodeProps,
  MarkdownInlineCodeProps,
} from "./render";
const defaultLabels: MarkdownLabels = {
  code: { copyLabel: "Copy", copiedLabel: "Copied" },
  footnotes: "Footnotes",
};

/** One settled full render: parse with math, resolve references, append the footnote section. */
function renderSettled(
  text: string,
  labels: MarkdownLabels,
  components: MarkdownComponents,
  omitHtml: boolean,
): ReactNode[] {
  const root = parseGfmWithMath(text);
  const targets = createReferenceTargets();
  collectReferenceTargets(root.children, targets);
  const context: MarkdownRenderContext = {
    streaming: false,
    labels,
    components,
    omitHtml,
    targets,
    footnoteOrder: [],
    footnoteCounts: new Map(),
  };
  const blocks = wrapBlockChildren(
    renderBlocks(
      root.children.map((node, index) => ({
        node,
        /* v8 ignore next -- parseFull uses parseGfm, which stamps every top-level node. */
        key: node.position?.start.offset ?? -(index + 1),
      })),
      context,
    ),
    false,
  );
  const section = renderFootnoteSection(context);
  return section === null ? blocks : [...blocks, "\n", section];
}

/**
 * Streaming render state for one growing message: the incremental parser,
 * the frozen blocks' cached elements, and the reference/footnote state their
 * rendering consumed (footnote numbering assigned to frozen references is
 * final, so the tail continues from a copy of it each frame).
 */
class StreamingRenderer {
  private readonly parser = new IncrementalMarkdownParser(parseGfm);
  private generation = -1;
  private frozenCount = 0;
  private frozenElements: ReactNode[] = [];
  private frozenTargets: ReferenceTargets = createReferenceTargets();
  private frozenFootnoteOrder: string[] = [];
  private frozenFootnoteCounts = new Map<string, number>();
  private lastText: string | null = null;
  private lastRendered: ReactNode[] = [];

  /**
   * @param labels - Localized Markdown chrome baked into cached elements; the owner replaces the renderer when it changes.
   * @param omitHtml - Whether raw HTML renders nothing; the owner replaces the renderer when it changes.
   */
  constructor(
    private readonly labels: MarkdownLabels,
    readonly omitHtml: boolean,
    readonly components: MarkdownComponents,
    readonly streaming: boolean,
  ) {}

  /**
   * Render the current accumulated text. Idempotent per text value, so React
   * may re-execute the calling render freely.
   * @param text - The full accumulated markdown source.
   * @returns Frozen elements, re-rendered tail, and the footnote section.
   */
  render(text: string): ReactNode[] {
    if (text === this.lastText) return this.lastRendered;
    const { frozen, tail, generation } = this.parser.update(text);
    if (generation !== this.generation) {
      this.generation = generation;
      this.frozenCount = 0;
      this.frozenElements = [];
      this.frozenTargets = createReferenceTargets();
      this.frozenFootnoteOrder = [];
      this.frozenFootnoteCounts = new Map();
    }
    const newlyFrozen = frozen.slice(this.frozenCount);
    collectReferenceTargets(
      newlyFrozen.map((block) => block.node),
      this.frozenTargets,
    );
    // Targets visible this frame: everything frozen so far plus the current
    // tail parse — a newly frozen block's references resolved against the
    // same parse tree its definitions came from.
    const frameTargets: ReferenceTargets = {
      definitions: new Map(this.frozenTargets.definitions),
      footnotes: new Map(this.frozenTargets.footnotes),
    };
    collectReferenceTargets(
      tail.map((block) => block.node),
      frameTargets,
    );
    if (newlyFrozen.length > 0) {
      const frozenContext: MarkdownRenderContext = {
        streaming: true,
        labels: this.labels,
        components: this.components,
        omitHtml: this.omitHtml,
        targets: frameTargets,
        footnoteOrder: this.frozenFootnoteOrder,
        footnoteCounts: this.frozenFootnoteCounts,
      };
      // Separator newlines are cached alongside the elements so the
      // assembled children match the settled pipeline's block wrapping.
      const batch = [...this.frozenElements];
      for (const element of renderBlocks(newlyFrozen, frozenContext)) {
        if (batch.length > 0) batch.push("\n");
        batch.push(element);
      }
      this.frozenElements = batch;
      this.frozenCount = frozen.length;
    }
    const tailContext: MarkdownRenderContext = {
      streaming: true,
      labels: this.labels,
      components: this.components,
      omitHtml: this.omitHtml,
      targets: frameTargets,
      footnoteOrder: [...this.frozenFootnoteOrder],
      footnoteCounts: new Map(this.frozenFootnoteCounts),
    };
    const children = [...this.frozenElements];
    for (const element of renderBlocks(tail, tailContext)) {
      if (children.length > 0) children.push("\n");
      children.push(element);
    }
    const section = renderFootnoteSection(tailContext);
    if (section !== null) children.push("\n", section);
    this.lastText = text;
    this.lastRendered = children;
    return this.lastRendered;
  }
}

/** 单一 Markdown 入口；稳定前缀缓存，完成后全量恢复数学、跨块引用及脚注。 */
export const MarkdownText = memo(function MarkdownText({
  text,
  streaming = false,
  labels = defaultLabels,
  components = emptyComponents,
  className,
  ...props
}: HTMLAttributes<HTMLDivElement> & {
  text: string;
  streaming?: boolean;
  labels?: MarkdownLabels;
  components?: MarkdownComponents;
}) {
  // 缓存对象只保存可丢弃的解析投影；非追加输入自行复位，重渲染可幂等重放。
  const renderer = useMemo(
    () => new StreamingRenderer(labels, false, components, streaming),
    [labels, components, streaming],
  );
  const children = useMemo(
    () =>
      streaming
        ? renderer.render(text)
        : renderSettled(text, labels, components, false),
    [text, streaming, renderer, labels, components],
  );
  return (
    <div
      className={cn(
        "min-w-0 break-words [&_li]:my-1 [&_ol]:list-decimal [&_ol]:pl-6 [&_strong]:font-semibold [&_ul]:list-disc [&_ul]:pl-6",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
});
