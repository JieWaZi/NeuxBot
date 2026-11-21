import { createContext, useContext } from "react";

const WorkbenchOpening = createContext<
  { scopeKey: string; onOpen: () => void } | undefined
>(undefined);

export const WorkbenchOpeningProvider = WorkbenchOpening.Provider;

/** 嵌入式输入复用所属聊天的真实工作范围及工作台打开入口。 */
export function useChatWorkspaceHost() {
  return useContext(WorkbenchOpening);
}
