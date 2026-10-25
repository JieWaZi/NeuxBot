import type { ReactNode } from "react";

type Section =
  | "resources"
  | "plugins"
  | "connectors"
  | "chats"
  | "memory"
  | "overview"
  | "projects"
  | "skills";

type Props = {
  active: Section;
  children: ReactNode;
};

export function WakerDetailLayout({ active, children }: Props) {
  return (
    <main
      className="bg-surface-scene h-full min-w-0 overflow-hidden"
      data-partner-section={active}
    >
      {children}
    </main>
  );
}
