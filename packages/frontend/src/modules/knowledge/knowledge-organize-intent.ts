import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { Source } from "./knowledge-model";
import type { KnowledgeOrganizeStart } from "./knowledge-organize-start";

type IntentState = {
  draft?: { sources: Source[]; batch: string[]; requirements: string };
  start?: KnowledgeOrganizeStart;
  frozen?: ExecutionSelection;
  remoteCommand?: {
    profileId?: string;
    sourceVersionIds: string[];
    instruction: string;
    expectedVersion: number;
    structureVersion: number;
    rebuild: boolean;
  };
};
export type KnowledgeOrganizeIntent = {
  id: string;
  get: () => IntentState;
  update: (patch: IntentState) => void;
};

/** 页面同步保留冻结输入，实际执行模块随整理弹窗按需加载。 */
export function createKnowledgeOrganizeIntent(): KnowledgeOrganizeIntent {
  let state: IntentState = {};
  return {
    id: crypto.randomUUID(),
    get: () => state,
    update: (patch) => {
      state = { ...state, ...patch };
    },
  };
}
