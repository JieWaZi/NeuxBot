import { knowledgeAdapter, type ResourceRef } from "@/shared/api/knowledge";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import type { GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as AcceptedExecution } from "@/shared/api/generated/types.gen";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";
import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import { knowledgeWikiZh } from "@/shared/i18n/knowledge-wiki";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";

type SendOptions = Parameters<
  ReturnType<typeof knowledgeAdapter>["sendMaintenanceMessage"]
>[0];
/** 维护发送只固定接纳身份；流、队列、审批与取消继续由普通 ChatPane 负责。 */
export function createWikiMaintenanceSender({
  resource,
  pageId,
  expectedVersion,
  perform,
}: {
  resource: ResourceRef;
  pageId: string;
  expectedVersion: number;
  perform?: (options: SendOptions) => Promise<{ data: AcceptedExecution }>;
}) {
  const ref = { ...resource };
  const adapter = knowledgeAdapter(ref);
  const target = { pageId, expectedVersion };
  const execute = perform ?? adapter.sendMaintenanceMessage;
  let chatId: string | undefined;
  let versionRejected = false;
  let pending:
    | { signature: string; options: SendOptions; outcomeUnknown?: boolean }
    | undefined;
  return {
    /** 仅首次接纳被明确拒绝的会话，允许重新打开入口采用当前页版本。 */
    canRefreshRejectedVersion(): boolean {
      return versionRejected && !chatId && !pending;
    },
    async send(
      text: string,
      attachments: string[],
      selection: ExecutionSelection,
    ): Promise<AcceptedExecution> {
      if (attachments.length)
        throw new ApiError(
          i18n.t("knowledgeWiki:maintenanceAttachments", {
            defaultValue: knowledgeWikiZh.maintenanceAttachments,
          }),
          "KnowledgeInvalid",
          400,
        );
      const input = {
        text,
        runtimeId: selection.runtimeId ?? "",
        accountId: selection.accountId,
        model: selection.model,
        reasoning: selection.reasoning,
        permissionLevel:
          permissionLevelOf(selection.permissionLevel) ?? "default",
      };
      const signature = JSON.stringify(input);
      if (pending && pending.signature !== signature)
        throw new ApiError(
          i18n.t("knowledge:operationConflict", {
            defaultValue: knowledgeZh.operationConflict,
          }),
          "KnowledgeConflict",
          409,
        );
      pending ??= {
        signature,
        options: {
          path: { libraryId: ref.knowledgeBaseId },
          body: { requestId: crypto.randomUUID(), chatId, ...target, ...input },
        },
      };
      const request = pending;
      try {
        versionRejected = false;
        const result = (await execute(request.options)).data;
        if (!result.chat?.id || !result.execution?.id)
          throw new ApiError(
            i18n.t("knowledgeWiki:maintenanceFailed", {
              defaultValue: knowledgeWikiZh.maintenanceFailed,
            }),
            "KnowledgeRemoteOutcomeUnknown",
            504,
          );
        chatId = result.chat.id;
        pending = undefined;
        return result;
      } catch (cause) {
        if (isKnowledgeWriteOutcomeUnknown(cause)) {
          request.outcomeUnknown = true;
        } else if (!request.outcomeUnknown) {
          const versionConflict =
            cause instanceof ApiError &&
            cause.status === 409 &&
            ["KnowledgeConflict", "40900009501"].includes(cause.code);
          versionRejected = !chatId && versionConflict;
          pending = undefined;
        }
        // 原响应丢失后的拒绝可能先于接纳回执查询，继续核对同一请求。
        throw cause;
      }
    },
  };
}
