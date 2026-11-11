import {
  createContext,
  useContext,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { cn } from "@/shared/lib/cn";

const FooterHost = createContext<HTMLDivElement | null | undefined>(undefined);
/** 宿主与正文同属设置pane，footer不参与正文滚动。 */
export function SettingsPageFooterHost({ children }: { children: ReactNode }) {
  const [host, setHost] = useState<HTMLDivElement | null>(null);
  return (
    <FooterHost.Provider value={host}>
      {children}
      <div
        ref={setHost}
        data-slot="settings-page-footer-host"
        className="shrink-0"
      />
    </FooterHost.Provider>
  );
}
/** 无宿主时内联，允许领域表单在独立测试或其他宿主中复用。 */
export function SettingsPageFooter({
  className,
  children,
  ...props
}: ComponentProps<"footer">) {
  const host = useContext(FooterHost);
  const footer = (
    <footer
      data-slot="settings-page-footer"
      {...props}
      className={cn(
        "bg-resource-surface border-border-subtle min-h-15 shrink-0 border-t",
        className,
      )}
    >
      <div className="max-w-settings-content mx-auto flex w-full min-w-0 flex-wrap items-center justify-between gap-3 px-4 py-3 @[42.5rem]/settings-scene:px-8">
        {children}
      </div>
    </footer>
  );
  return host === undefined ? footer : host ? createPortal(footer, host) : null;
}
