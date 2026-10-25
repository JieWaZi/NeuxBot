import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { GithubComJieWaZiNeuxBotServerApisKnowledgeOrganizeResponse as OrganizeResponse } from "@/shared/api/generated/types.gen";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { Library } from "./knowledge-model";
import type { KnowledgePerform } from "./knowledge-writes";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";

import type { KnowledgeOrganizeIntent } from "./knowledge-organize-intent";
export {
  createKnowledgeOrganizeIntent,
  type KnowledgeOrganizeIntent,
} from "./knowledge-organize-intent";
export type KnowledgeOrganizeStart = {
  id: string;
  selection: ExecutionSelection;
  message: string;
  execute: () => Promise<OrganizeResponse>;
  accepted: () => OrganizeResponse | undefined;
};

/** 同一冻结后台命令复用接纳请求与回执，关闭配置弹窗不取消实际执行。 */
export function createKnowledgeOrganizeStart({
  library,
  batch,
  selection,
  intent,
  perform,
  message,
  notAcceptedMessage,
  rebuild = false,
}: {
  library: Library;
  batch: string[];
  selection: ExecutionSelection;
  intent: KnowledgeOrganizeIntent;
  perform: KnowledgePerform;
  message: string;
  notAcceptedMessage?: string;
  rebuild?: boolean;
}): KnowledgeOrganizeStart {
  const retained = intent.get().start;
  if (retained) return retained;
  const chosen = intent.get().frozen ?? selection;
  intent.update({ frozen: chosen });
  let receipt: OrganizeResponse | undefined;
  let pending: Promise<OrganizeResponse> | undefined;
  const run = async () => {
    try {
      let accepted: OrganizeResponse = {};
      await perform(
        `${rebuild ? "rebuild" : "organize"}:${library.id}:${intent.id}`,
        async (requestId) => {
          const response = await knowledgeAdapter(library.ref).organize({
            path: { libraryId: library.id },
            body: {
              requestId,
              ...(rebuild ? { rebuild: true } : {}),
              sourceVersionIds: [...batch],
              runtimeId: chosen.runtimeId,
              accountId: chosen.accountId,
              model: chosen.model,
              message,
            },
            throwOnError: true,
          });
          accepted = response.data;
          if (
            (rebuild || !accepted.noop) &&
            (!accepted.receipt?.jobId ||
              accepted.receipt.resourceId !== accepted.receipt.jobId ||
              accepted.receipt.requestId !== requestId ||
              accepted.receipt.libraryId !== library.id ||
              accepted.receipt.state !== "accepted" ||
              !(Number(accepted.receipt.version) > 0))
          )
            throw new Error(
              notAcceptedMessage || "Missing accepted knowledge job",
            );
        },
      );
      receipt = accepted;
      return accepted;
    } catch (cause) {
      if (!isKnowledgeWriteOutcomeUnknown(cause))
        intent.update({
          frozen: undefined,
          start: undefined,
        });
      throw cause;
    }
  };
  const start: KnowledgeOrganizeStart = {
    id: intent.id,
    selection: chosen,
    message,
    accepted: () => receipt,
    execute: () => {
      if (receipt) return Promise.resolve(receipt);
      if (!pending)
        pending = run().catch((cause) => {
          pending = undefined;
          throw cause;
        });
      return pending;
    },
  };
  intent.update({ start });
  return start;
}
