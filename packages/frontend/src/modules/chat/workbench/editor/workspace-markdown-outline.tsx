import { useEffect, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";

type Heading = { element: HTMLElement; depth: number; label: string };

/** 扫描当前 Markdown 标题，为工作台提供可定位的大纲。 */
export function WorkspaceMarkdownOutline({
  scrollContainer,
}: {
  scrollContainer: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation("chat");
  const [headings, setHeadings] = useState<Heading[]>([]);

  useEffect(() => {
    const container = scrollContainer.current;
    if (!container) return;
    const scan = () => {
      const next = Array.from(
        container.querySelectorAll<HTMLElement>(
          "[data-workspace-markdown] h1, [data-workspace-markdown] h2, [data-workspace-markdown] h3, [data-workspace-markdown] h4, [data-workspace-markdown] h5, [data-workspace-markdown] h6",
        ),
      ).flatMap((element) => {
        const label = element.textContent?.trim() ?? "";
        return label
          ? [{ element, depth: Number(element.tagName.slice(1)), label }]
          : [];
      });
      setHeadings((current) =>
        current.length === next.length &&
        current.every(
          (entry, index) =>
            entry.element === next[index]?.element &&
            entry.label === next[index]?.label,
        )
          ? current
          : next,
      );
    };
    const observer = new MutationObserver(scan);
    observer.observe(container, {
      childList: true,
      subtree: true,
      characterData: true,
    });
    queueMicrotask(scan);
    return () => observer.disconnect();
  }, [scrollContainer]);

  return (
    <aside className="border-border w-56 shrink-0 overflow-y-auto border-r p-3">
      <p className="text-muted-foreground type-caption mb-2 font-medium">
        {t("workspace.outline")}
      </p>
      <div className="grid gap-1">
        {headings.map((heading, index) => (
          <button
            key={`${index}:${heading.label}`}
            type="button"
            className="hover:bg-muted type-ui min-h-row-compact truncate rounded-sm px-2 text-left"
            style={{ paddingLeft: `${8 + (heading.depth - 1) * 12}px` }}
            onClick={() => {
              const container = scrollContainer.current;
              if (!container || !heading.element.isConnected) return;
              heading.element
                .closest("details:not([open])")
                ?.setAttribute("open", "");
              const top =
                container.scrollTop +
                heading.element.getBoundingClientRect().top -
                container.getBoundingClientRect().top;
              container.scrollTo({ top, behavior: "smooth" });
            }}
          >
            {heading.label}
          </button>
        ))}
      </div>
    </aside>
  );
}
