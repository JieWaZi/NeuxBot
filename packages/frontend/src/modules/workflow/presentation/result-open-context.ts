import { createContext } from "react";
import type { GithubComJieWaZiNeuxBotServerApisWorkflowsWorkflowNodeResultReference as ResultReference } from "@/shared/api/generated/types.gen";

// 上下文独立于阅读组件的热更新，保留的画布与更新后的产出入口共用同一身份。
export const OpenResult = createContext<
  (ref: ResultReference, fileId?: string, title?: string) => void
>(() => {});
