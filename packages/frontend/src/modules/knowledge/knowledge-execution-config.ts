import type { CreateLibraryData } from "@/shared/api/generated/types.gen";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";

/** 同步构造创建请求，不让页面为草稿初始化加载配置查询模块。 */
export function buildExecutionDefaults(
  selection: ExecutionSelection,
): NonNullable<CreateLibraryData["body"]["executionDefaults"]> {
  return {
    runtimeId: selection.runtimeId ?? "",
    accountId: selection.accountId ?? "",
    model: selection.model ?? "",
    reasoning: selection.reasoning ?? "",
  };
}
