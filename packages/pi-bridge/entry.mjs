// 固定上游源码入口，只导入分类能力，不加载可由用户关闭的自动审批命令入口。
export { createMcpAdapter } from "./node_modules/pi-mcp-adapter/index.ts";
export { classifyAction } from "./node_modules/pi-auto-approval/src/classifier.ts";
export { createReviewSubject } from "./node_modules/pi-auto-approval/src/tool-routing.ts";
export { completeSimple } from "@earendil-works/pi-ai";
