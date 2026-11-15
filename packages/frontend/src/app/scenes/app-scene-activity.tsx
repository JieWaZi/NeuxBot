import { Activity, type ReactNode } from "react";
import type { AppSceneBackground } from "@/shared/workspace/app-scene-model";

/** 资源场景保留草稿与 DOM，隐藏后释放消费并把更新让给前台交互。 */
export function AppSceneActivity({
  active,
  children,
  background,
}: {
  active: boolean;
  children: ReactNode;
  background?: AppSceneBackground;
}) {
  // 会话、工作流及未知场景沿用原生命周期，避免回收仍需保持身份的原生视图。
  return (
    <Activity mode={!active && background === "suspend" ? "hidden" : "visible"}>
      {children}
    </Activity>
  );
}
