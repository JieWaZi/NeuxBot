// Adapted from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
/** 直接渲染 mdast；上层只注入链接、图片、表格和代码的现有产品入口。 */
import {
  Fragment,
  createElement,
  type ComponentType,
  type ComponentPropsWithoutRef,
  type Key,
  type ReactNode,
} from "react";
import type * as Md from "mdast";
import type {} from "mdast-util-math";
import { normalizeUri } from "micromark-util-sanitize-uri";
import { CodeBlock } from "./CodeBlock";
import { renderTexToReact } from "./katex";
import type { PositionedBlock } from "./incremental";

export interface MarkdownCodeLabels {
  copyLabel: string;
  copiedLabel: string;
}
export interface MarkdownLabels {
  code: MarkdownCodeLabels;
  footnotes: string;
}
export interface MarkdownCodeProps {
  code: string;
  language?: string;
  streaming: boolean;
}
export interface MarkdownInlineCodeProps {
  children: string;
  inLink: boolean;
}
export interface MarkdownComponents {
  link?: ComponentType<ComponentPropsWithoutRef<"a">>;
  inlineCode?: ComponentType<MarkdownInlineCodeProps>;
  code?: ComponentType<MarkdownCodeProps>;
  table?: ComponentType<ComponentPropsWithoutRef<"table">>;
  image?: ComponentType<ComponentPropsWithoutRef<"img">>;
}
const emptyComponents: MarkdownComponents = {};
export { emptyComponents };
/** Link/image reference targets collected from a document (first definition per identifier wins, as in CommonMark). */
export interface ReferenceTargets {
  /** Link/image definitions keyed by upper-cased identifier. */
  definitions: Map<string, Md.Definition>;
  /** Footnote definitions keyed by upper-cased identifier. */
  footnotes: Map<string, Md.FootnoteDefinition>;
}

/**
 * Create an empty {@link ReferenceTargets}.
 * @returns Fresh empty maps.
 */
export function createReferenceTargets(): ReferenceTargets {
  return { definitions: new Map(), footnotes: new Map() };
}

/**
 * Record every definition and footnote definition under `nodes` into
 * `targets`, depth-first, keeping the first definition per identifier.
 * @param nodes - Subtrees to walk (top-level blocks or any nested children).
 * @param targets - Accumulator, typically shared across incremental segments.
 */
export function collectReferenceTargets(
  nodes: readonly Md.RootContent[],
  targets: ReferenceTargets,
): void {
  for (const node of nodes) {
    if (node.type === "definition") {
      const id = node.identifier.toUpperCase();
      if (!targets.definitions.has(id)) targets.definitions.set(id, node);
    } else if (node.type === "footnoteDefinition") {
      const id = node.identifier.toUpperCase();
      if (!targets.footnotes.has(id)) targets.footnotes.set(id, node);
    }
    if ("children" in node) collectReferenceTargets(node.children, targets);
  }
}

/**
 * One render pass's state: immutable options and targets plus the footnote
 * numbering accumulated in document order while references render.
 */
export interface MarkdownRenderContext {
  /** Streaming arm: fences highlight incrementally as they grow; TeX (including ```math fences) stays literal until the settled pass. */
  readonly streaming: boolean;
  /** Localized fence copy-button labels. */
  readonly labels: MarkdownLabels;
  /** Inside a blockquote's children: tables there always fill the quote's width. */
  readonly inBlockquote?: boolean;
  readonly components: MarkdownComponents;
  /** Raw HTML renders nothing instead of literal text. */
  readonly omitHtml: boolean;
  /** Inside an anchor's children: interactive mentions must not nest there. */
  readonly inLink?: boolean;
  /** Reference targets visible to this pass. */
  readonly targets: ReferenceTargets;
  /** Footnote identifiers in first-reference order; a footnote's number is its 1-based index here. */
  readonly footnoteOrder: string[];
  /** References rendered per identifier; drives the section's back-reference count. */
  readonly footnoteCounts: Map<string, number>;
}

/**
 * Render top-level blocks. Nodes that render nothing (definitions, unmapped
 * types) are dropped rather than kept as null placeholders, matching the
 * replaced pipeline's child lists so separator newlines land identically.
 * @param blocks - Blocks with their stream-stable render keys.
 * @param context - The pass state; footnote numbering mutates in document order.
 * @returns One React node per rendered block.
 */
export function renderBlocks(
  blocks: readonly PositionedBlock[],
  context: MarkdownRenderContext,
): ReactNode[] {
  return blocks
    .map((block) => renderNode(block.node, block.key, context))
    .filter((element) => element !== null);
}

/**
 * Interleave the newline text nodes the replaced pipeline emitted between
 * block-level children. They are invisible between elements but coalesce
 * into adjacent literal raw-HTML text, where the DOM parity fixtures pin
 * them.
 * @param elements - Rendered block children with empty renders already dropped.
 * @param edges - Also emit the leading and trailing newline (hast's loose wrap).
 * @returns The interleaved children.
 */
export function wrapBlockChildren(
  elements: readonly ReactNode[],
  edges: boolean,
): ReactNode[] {
  const wrapped: ReactNode[] = [];
  for (const element of elements) {
    if (edges || wrapped.length > 0) wrapped.push("\n");
    wrapped.push(element);
  }
  if (edges && elements.length > 0) wrapped.push("\n");
  return wrapped;
}

/**
 * A block child rendered for a parent that must tell paragraphs apart from
 * other blocks (list items unwrap them when tight; footnote bodies receive
 * their back-references inside the trailing paragraph).
 */
type BlockEntry = { paragraph: ReactNode[] } | { element: ReactNode };

/** Render container children into {@link BlockEntry} values, dropping empty renders. */
function renderBlockEntries(
  blocks: readonly Md.RootContent[],
  context: MarkdownRenderContext,
): BlockEntry[] {
  const entries: BlockEntry[] = [];
  for (const [index, block] of blocks.entries()) {
    if (block.type === "paragraph") {
      entries.push({ paragraph: renderChildren(block.children, context) });
    } else {
      const element = renderNode(block, index, context);
      if (element !== null) entries.push({ element });
    }
  }
  return entries;
}

function renderChildren(
  nodes: readonly Md.RootContent[],
  context: MarkdownRenderContext,
): ReactNode[] {
  return nodes.map((node, index) => renderNode(node, index, context));
}

function renderNode(
  node: Md.RootContent,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  switch (node.type) {
    case "text":
      return node.value;
    case "paragraph":
      return <p key={key}>{renderChildren(node.children, context)}</p>;
    case "heading":
      return createElement(
        `h${node.depth}`,
        { key },
        ...renderChildren(node.children, context),
      );
    case "blockquote":
      return (
        <blockquote key={key}>
          {wrapBlockChildren(
            renderChildren(node.children, {
              ...context,
              inBlockquote: true,
            }).filter((child) => child !== null),
            true,
          )}
        </blockquote>
      );
    case "thematicBreak":
      return <hr key={key} />;
    case "break":
      // The replaced pipeline emitted a newline text node after each <br>.
      return (
        <Fragment key={key}>
          <br />
          {"\n"}
        </Fragment>
      );
    case "strong":
      return (
        <strong key={key} data-streamdown="strong">
          {renderChildren(node.children, context)}
        </strong>
      );
    case "emphasis":
      return <em key={key}>{renderChildren(node.children, context)}</em>;
    case "delete":
      return <del key={key}>{renderChildren(node.children, context)}</del>;
    case "inlineCode": {
      const value = node.value.replace(/\r?\n|\r/g, " ");
      const InlineCode = context.components.inlineCode;
      return InlineCode ? (
        <InlineCode key={key} inLink={context.inLink === true}>
          {value}
        </InlineCode>
      ) : (
        <code key={key}>{value}</code>
      );
    }
    case "html":
      // No HTML parser enters the pipeline: raw HTML is literal text, or nothing where the owner omits it.
      return context.omitHtml ? null : node.value;
    case "code":
      return renderCode(node, key, context);
    case "math":
      return (
        <Fragment key={key}>{renderTexToReact(node.value, true)}</Fragment>
      );
    case "inlineMath":
      return (
        <Fragment key={key}>{renderTexToReact(node.value, false)}</Fragment>
      );
    case "list":
      return renderList(node, key, context);
    case "listItem":
      // Reachable only in hand-built trees: the grammar emits items inside lists.
      return renderListItem(node, listItemLoose(node), key, context);
    case "table":
      return renderTable(node, key, context);
    case "link":
      return renderAnchor(
        node.url,
        node.title,
        renderChildren(node.children, { ...context, inLink: true }),
        key,
        context,
      );
    case "linkReference":
      return renderLinkReference(node, key, context);
    case "image":
      return renderImage(node.url, node.alt ?? "", node.title, key, context);
    case "imageReference":
      return renderImageReference(node, key, context);
    case "footnoteReference":
      return renderFootnoteReference(node, key, context);
    case "definition":
    case "footnoteDefinition":
      // Targets render elsewhere: definitions resolve references in place;
      // footnote bodies render in the trailing section.
      return null;
    default:
      // Documented default for the merge-extensible union: node types without
      // a mapping (tableRow/tableCell outside a table, frontmatter, future
      // grammar contributions) render nothing.
      return null;
  }
}

function renderCode(
  node: Md.Code,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const lang = node.lang?.match(/^[\w-]+/)?.[0];
  if (!context.streaming && lang === "math")
    return (
      <Fragment key={key}>{renderTexToReact(`${node.value}\n`, true)}</Fragment>
    );
  const CustomCode = context.components.code;
  return CustomCode ? (
    <CustomCode
      key={key}
      code={node.value}
      language={lang}
      streaming={context.streaming}
    />
  ) : (
    <CodeBlock
      key={key}
      code={node.value}
      lang={lang}
      streaming={context.streaming}
      {...context.labels.code}
    />
  );
}

/** A list is loose when it or any of its items is spread; every item then keeps its paragraphs. */
function listLoose(list: Md.List): boolean {
  return (list.spread ?? false) || list.children.some(listItemLoose);
}

function listItemLoose(item: Md.ListItem): boolean {
  return item.spread ?? item.children.length > 1;
}

function renderList(
  node: Md.List,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const loose = listLoose(node);
  const properties: { start?: number; className?: string } = {};
  if (typeof node.start === "number" && node.start !== 1)
    properties.start = node.start;
  if (node.children.some((item) => typeof item.checked === "boolean")) {
    properties.className = "contains-task-list";
  }
  return createElement(
    node.ordered === true ? "ol" : "ul",
    { key, ...properties },
    ...node.children.map((item, index) =>
      renderListItem(item, loose, index, context),
    ),
  );
}

function renderListItem(
  item: Md.ListItem,
  loose: boolean,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const entries = renderBlockEntries(item.children, context);
  const task = typeof item.checked === "boolean";
  if (task) {
    const checkbox = (
      <input
        key="task-checkbox"
        type="checkbox"
        checked={item.checked === true}
        disabled
      />
    );
    const head = entries[0];
    if (head !== undefined && "paragraph" in head) {
      head.paragraph =
        head.paragraph.length > 0
          ? [checkbox, " ", ...head.paragraph]
          : [checkbox];
    } else {
      entries.unshift({ paragraph: [checkbox] });
    }
  }
  // Newline placement and tight-paragraph unwrapping mirror
  // mdast-util-to-hast's list-item handler: a newline before every child
  // except a tight leading paragraph, and after a trailing non-paragraph
  // (or any trailing child when loose).
  const parts: ReactNode[] = [];
  for (const [index, entry] of entries.entries()) {
    const isParagraph = "paragraph" in entry;
    if (loose || index !== 0 || !isParagraph) parts.push("\n");
    if (!isParagraph) parts.push(entry.element);
    else if (loose) parts.push(<p key={`p-${index}`}>{entry.paragraph}</p>);
    else parts.push(<Fragment key={`p-${index}`}>{entry.paragraph}</Fragment>);
  }
  const tail = entries[entries.length - 1];
  if (tail !== undefined && (loose || !("paragraph" in tail))) parts.push("\n");
  return (
    <li key={key} className={task ? "task-list-item" : undefined}>
      {parts}
    </li>
  );
}

function renderTable(
  node: Md.Table,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const [headRow, ...bodyRows] = node.children;
  const Table = context.components.table ?? "table";
  return (
    <Table key={key}>
      {headRow && (
        <thead>
          {renderTableRow(headRow, "th", node.align ?? null, 0, context)}
        </thead>
      )}
      {bodyRows.length > 0 && (
        <tbody>
          {bodyRows.map((row, index) =>
            renderTableRow(row, "td", node.align ?? null, index + 1, context),
          )}
        </tbody>
      )}
    </Table>
  );
}

function renderTableRow(
  row: Md.TableRow,
  cellTag: "th" | "td",
  align: readonly Md.AlignType[] | null,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  // With column alignment present, every row renders exactly one cell per
  // column, padding or truncating the row (mdast-util-to-hast parity).
  const length = align === null ? row.children.length : align.length;
  const cells: ReactNode[] = [];
  for (let index = 0; index < length; index++) {
    const cell = row.children[index];
    const alignValue = align?.[index];
    cells.push(
      createElement(
        cellTag,
        // 对齐来自语法节点；保留属性让表格默认左对齐规则不覆盖显式列对齐。
        {
          key: index,
          "data-markdown-align": alignValue ?? undefined,
          className:
            alignValue === "right"
              ? "text-right"
              : alignValue === "center"
                ? "text-center"
                : alignValue === "left"
                  ? "text-left"
                  : undefined,
        },
        ...(cell === undefined ? [] : renderChildren(cell.children, context)),
      ),
    );
  }
  return <tr key={key}>{cells}</tr>;
}

/** 没有宿主适配器时仅允许普通外链；文件和图片必须由宿主明确授权。 */
function renderAnchor(
  url: string,
  title: string | null | undefined,
  children: ReactNode[],
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const Link = context.components.link;
  if (Link)
    return (
      <Link key={key} href={url} title={title ?? undefined}>
        {children}
      </Link>
    );
  if (!/^https?:\/\//i.test(url))
    return <Fragment key={key}>{children}</Fragment>;
  return (
    <a
      key={key}
      href={url}
      title={title ?? undefined}
      target="_blank"
      rel="noopener noreferrer"
      className="text-link decoration-link/50 hover:decoration-link focus-visible:ring-ring rounded-xs wrap-anywhere underline underline-offset-4 transition-colors outline-none focus-visible:ring-2"
    >
      {children}
    </a>
  );
}
function renderImage(
  url: string,
  alt: string,
  title: string | null | undefined,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const Image = context.components.image;
  return Image ? (
    <Image key={key} src={url} alt={alt} title={title ?? undefined} />
  ) : (
    <span key={key} title={title ?? undefined}>
      {alt}
    </span>
  );
}

/** The bracketed source text a reference reverts to when its definition is missing. */
function referenceSuffix(node: Md.LinkReference | Md.ImageReference): string {
  if (node.referenceType === "collapsed") return "][]";
  if (node.referenceType === "full")
    return `][${node.label ?? node.identifier}]`;
  return "]";
}

function renderLinkReference(
  node: Md.LinkReference,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const definition = context.targets.definitions.get(
    node.identifier.toUpperCase(),
  );
  if (definition === undefined) {
    // The grammar only emits references whose definitions exist somewhere in
    // the same parse, but incremental segments and hand-built trees may still
    // present unresolved ones: revert to the bracketed source text — which is
    // not an anchor, so mentions inside it stay live.
    return (
      <Fragment key={key}>
        {"["}
        {renderChildren(node.children, context)}
        {referenceSuffix(node)}
      </Fragment>
    );
  }
  const rendered = renderChildren(node.children, { ...context, inLink: true });
  return renderAnchor(definition.url, definition.title, rendered, key, context);
}

function renderImageReference(
  node: Md.ImageReference,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const definition = context.targets.definitions.get(
    node.identifier.toUpperCase(),
  );
  if (definition === undefined)
    return `![${node.alt ?? ""}${referenceSuffix(node)}`;
  return renderImage(
    definition.url,
    node.alt ?? "",
    definition.title,
    key,
    context,
  );
}

function renderFootnoteReference(
  node: Md.FootnoteReference,
  key: Key,
  context: MarkdownRenderContext,
): ReactNode {
  const id = node.identifier.toUpperCase();
  const seen = context.footnoteCounts.get(id);
  if (seen === undefined) context.footnoteOrder.push(id);
  context.footnoteCounts.set(id, (seen ?? 0) + 1);
  // The in-page anchor is not an absolute URL, so only the numbered
  // superscript renders (matching the replaced pipeline's unwrapped link).
  return <sup key={key}>{String(context.footnoteOrder.indexOf(id) + 1)}</sup>;
}

/**
 * Render the trailing footnote section for every footnote referenced during
 * the pass, in first-reference order, with one plain-text back-reference
 * marker per rendered reference.
 * @param context - The pass state after all blocks rendered.
 * @returns The section, or null when no referenced footnote has a definition.
 */
export function renderFootnoteSection(
  context: MarkdownRenderContext,
): ReactNode | null {
  const items: ReactNode[] = [];
  for (const id of context.footnoteOrder) {
    const definition = context.targets.footnotes.get(id);
    if (definition === undefined) continue;
    const count = context.footnoteCounts.get(id) ?? 0;
    const backrefs: ReactNode[] = [];
    for (let reference = 1; reference <= count; reference++) {
      if (backrefs.length > 0) backrefs.push(" ");
      backrefs.push("↩");
      if (reference > 1)
        backrefs.push(<sup key={`re-${reference}`}>{String(reference)}</sup>);
    }
    const entries = renderBlockEntries(definition.children, context);
    const tail = entries[entries.length - 1];
    const body: ReactNode[] = entries.map((entry, index) =>
      "paragraph" in entry ? (
        <p key={`p-${index}`}>
          {entry.paragraph}
          {entry === tail && <> {backrefs}</>}
        </p>
      ) : (
        entry.element
      ),
    );
    // Without a trailing paragraph the back-references join the block list
    // itself (and pick up the wrap newlines), as in the replaced pipeline.
    if (tail === undefined || !("paragraph" in tail)) body.push(...backrefs);
    items.push(
      <li key={id} id={`user-content-fn-${normalizeUri(id.toLowerCase())}`}>
        {wrapBlockChildren(body, true)}
      </li>,
    );
  }
  if (items.length === 0) return null;
  return (
    <section key="footnotes" data-footnotes className="footnotes">
      <h2 id="footnote-label" className="sr-only">
        {context.labels.footnotes}
      </h2>
      <ol>{items}</ol>
    </section>
  );
}
