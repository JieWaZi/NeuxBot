import { Slot } from "@radix-ui/react-slot";
import type { ComponentProps, CSSProperties, ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

/** Dock 悬浮于内容之上；避让空间只交给滚动内容，不生成底部占位栏。 */
export function DetailDockLayout({
  children,
  navigation,
}: {
  children: ReactNode;
  navigation?: ReactNode;
}) {
  return (
    <div
      data-slot="detail-dock-layout"
      className="relative h-full min-h-0 min-w-0 overflow-hidden"
      style={
        navigation
          ? ({
              "--scene-overlay-clearance": "var(--shell-detail-dock-clearance)",
            } as CSSProperties)
          : undefined
      }
    >
      {children}
      {navigation && (
        <div
          data-slot="detail-dock-overlay"
          className="pointer-events-none absolute inset-x-0 bottom-4 z-30 flex justify-center px-4"
        >
          {navigation}
        </div>
      )}
    </div>
  );
}

/** 嵌套详情 pane 的滚动区，统一保留可滚动的 Dock 避让空间。 */
export function DetailDockScrollArea({
  asChild = false,
  className,
  ...props
}: ComponentProps<"div"> & { asChild?: boolean }) {
  const Component = asChild ? Slot : "div";
  return (
    <Component
      {...props}
      className={cn(
        "scrollbar-hidden pb-scene-bottom scroll-pb-scene-bottom min-h-0 min-w-0 overflow-y-auto",
        className,
      )}
    />
  );
}
