type SourceLineRange = { startLine: number; endLine: number };

export function findSelectedTextLineRange(
  content: string,
  selectedText: string,
): SourceLineRange | undefined {
  const selected = selectedText.trim();
  const index = selected ? content.indexOf(selected) : -1;
  // Rendered text must identify an unambiguous location in the source.
  if (index < 0 || content.indexOf(selected, index + 1) >= 0) return undefined;
  const startLine = content.slice(0, index).split("\n").length;
  return { startLine, endLine: startLine + selected.split("\n").length - 1 };
}

export function readWorkspaceTextSelection(
  container: HTMLElement,
  range: Range,
  source: string,
): (SourceLineRange & { content: string }) | undefined {
  if (range.collapsed || !container.contains(range.commonAncestorContainer))
    return undefined;
  const codeLines = container.querySelectorAll<HTMLElement>("[data-code-line]");
  if (!codeLines.length) {
    const content = range.toString();
    const lines = findSelectedTextLineRange(source, content);
    return lines ? { ...lines, content } : undefined;
  }

  let content = "";
  let startLine = 0;
  let endLine = 0;
  for (const element of codeLines) {
    const part = document.createRange();
    part.selectNodeContents(element);
    const start = part.comparePoint(range.startContainer, range.startOffset);
    const end = part.comparePoint(range.endContainer, range.endOffset);
    if (start > 0 || end < 0) continue;
    if (start === 0) part.setStart(range.startContainer, range.startOffset);
    if (end === 0) part.setEnd(range.endContainer, range.endOffset);
    const text = part.toString();
    if (!text) continue;
    const line = Number(
      element.closest<HTMLElement>("[data-line]")?.dataset.line,
    );
    if (!Number.isInteger(line) || line < 1) return undefined;
    startLine ||= line;
    endLine = line;
    content += text;
  }
  return content.trim() ? { content, startLine, endLine } : undefined;
}
