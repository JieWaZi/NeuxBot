import { ApiError } from "@/shared/api/errors";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import type {
  CreateChatData,
  CreateChatResponse,
} from "@/shared/api/generated/types.gen";
import type { PreparedChatSubmission } from "../runtime/chat-queue-store";

// 这些错误来自权威重放检查之后、接纳之前；同一请求没有提交。
const admissionRejections = new Set([
  "RuntimeUnavailable",
  "RuntimeConfigurationInvalid",
  "RuntimePolicyUnsupported",
  "RuntimeModelSelectionFailed",
  "RuntimeAuthRequired",
  "AIAccountConfigurationInvalid",
  "WakerNotFound",
  "ProjectNotFound",
  "MessageAttachmentInvalid",
  "SkillUnavailable",
]);

/** 首发只能确认原命令或重放同一份输入，不能重新解析草稿配置。 */
export function prepareFirstChatSubmission(
  body: CreateChatData["body"],
  send: (body: CreateChatData["body"]) => Promise<CreateChatResponse>,
  finished: () => void,
): PreparedChatSubmission {
  let uncertain = false;
  let complete = false;

  function finish() {
    if (complete) return;
    uncertain = false;
    complete = true;
    finished();
  }

  return {
    submissionId: body.clientRequestId,
    hasUnconfirmedReceipt: () => uncertain,
    confirmAccepted: finish,
    accept: async () => {
      try {
        const result = await send(body);
        if (!result.chat?.id) throw new Error("Missing create chat receipt");
        return result;
      } catch (error) {
        const code =
          error instanceof ApiError
            ? applicationErrorKey(error.code)
            : undefined;
        const rejected =
          (code !== undefined && admissionRejections.has(code)) ||
          (!uncertain &&
            error instanceof ApiError &&
            error.status !== undefined &&
            error.status >= 400 &&
            error.status < 500 &&
            error.status !== 408 &&
            code !== "CreateRequestInProgress" &&
            code !== "CreateRequestConflict" &&
            code !== "ChatHasActiveExecution");
        if (rejected) finish();
        else uncertain = true;
        throw error;
      }
    },
  };
}
