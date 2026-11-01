import { createContext } from "react";

/** 保活但隐藏的 Scene 不启动可见内容计算或按需读取。 */
export const ContentVisibility = createContext(true);
