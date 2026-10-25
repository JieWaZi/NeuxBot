/** Message order is monotonic, so locate the reading turn without measuring every row. */
export function readingTurnIndex(
  viewport: HTMLElement,
  turnIds: readonly string[],
): number {
  const byId = new Map(
    Array.from(
      viewport.querySelectorAll<HTMLElement>("[data-message-id]"),
      (element) => [element.dataset.messageId, element] as const,
    ),
  );
  const rows = turnIds.flatMap((id, index) => {
    const element = byId.get(id);
    return element ? [{ element, index }] : [];
  });
  const readingLine =
    viewport.getBoundingClientRect().top + viewport.clientHeight * 0.3;
  let low = 0;
  let high = rows.length - 1;
  let activeIndex = 0;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const row = rows[middle];
    if (!row) break;
    if (row.element.getBoundingClientRect().top <= readingLine) {
      activeIndex = row.index;
      low = middle + 1;
    } else high = middle - 1;
  }
  return activeIndex;
}
