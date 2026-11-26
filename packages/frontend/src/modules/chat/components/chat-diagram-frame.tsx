import type { ReactNode, Ref } from "react";

/** 聊天图表共用预览表面和操作位置。 */
export function ChatDiagramFrame({
  kind,
  frameRef,
  leading,
  actions,
  children,
}: {
  kind: "mermaid" | "excalidraw";
  frameRef?: Ref<HTMLElement>;
  leading?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section
      ref={frameRef}
      className="bg-muted my-3 min-w-0 rounded-lg p-3"
      data-streamdown={`${kind}-block`}
    >
      {actions || leading ? (
        <header className="mb-3 flex items-center justify-between gap-3">
          {leading}
          <div className="ml-auto flex items-center gap-1">{actions}</div>
        </header>
      ) : null}
      {children}
    </section>
  );
}
