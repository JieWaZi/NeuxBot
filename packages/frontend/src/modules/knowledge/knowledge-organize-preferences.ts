import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { resourceKey, type ResourceRef } from "./knowledge-transfer";

type OrganizePreferences = {
  selection?: Pick<
    ExecutionSelection,
    "runtimeId" | "accountId" | "model" | "reasoning"
  >;
  requirements?: string;
  profileId?: string;
};

// 仅在当前前端运行期间复用；重启后重新读取知识库设置。
const preferences = new Map<string, OrganizePreferences>();

export function readOrganizePreferences(ref: ResourceRef): OrganizePreferences {
  const value = preferences.get(resourceKey(ref));
  return value
    ? { ...value, selection: value.selection && { ...value.selection } }
    : {};
}

/** 只暂存表单偏好，来源范围与待核验请求仍由整理意图持有。 */
export function saveOrganizePreferences(
  ref: ResourceRef,
  patch: OrganizePreferences,
) {
  const selection = patch.selection;
  preferences.set(resourceKey(ref), {
    ...readOrganizePreferences(ref),
    ...patch,
    ...(selection
      ? {
          selection: {
            runtimeId: selection.runtimeId,
            accountId: selection.accountId,
            model: selection.model,
            reasoning: selection.reasoning,
          },
        }
      : {}),
  });
}
