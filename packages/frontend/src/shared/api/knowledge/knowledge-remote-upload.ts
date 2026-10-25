import * as api from "@/shared/api/generated/sdk.gen";
import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import {
  assertKnowledgeCapability,
  scopedKnowledgeOptions,
  knowledgeUnsupported,
  type KnowledgeCapabilities,
  type KnowledgeOptions,
  type KnowledgeUploadContext,
  type ResourceRef,
} from "./knowledge-adapter-types";
import { getRemoteKnowledgeCapabilities } from "./knowledge-remote-operations";

/** 用户确认的是已观察的处理模式和连接代次；新模式必须重新呈现后才能发送。 */
export function remoteUploadContext(
  capabilities: KnowledgeCapabilities,
): KnowledgeUploadContext {
  if (
    !capabilities.connectionRevision ||
    !["separate", "automatic"].includes(capabilities.uploadMode ?? "")
  )
    knowledgeUnsupported();
  if (capabilities.uploadMode === "separate") {
    assertKnowledgeCapability(capabilities, "rawOnly");
    return {
      uploadMode: "separate",
      connectionRevision: capabilities.connectionRevision,
    };
  }
  const upload = capabilities.details.upload;
  if (
    !upload?.supported ||
    upload.permission === "denied" ||
    !["ready", "automatic_processing"].includes(upload.condition)
  )
    knowledgeUnsupported();
  // 第三方没有权限自省时，真实上传由原服务器裁决；这里不通过试写探测权限。
  return {
    uploadMode: "automatic",
    connectionRevision: capabilities.connectionRevision,
  };
}

export async function uploadRemoteKnowledgeSource(
  ref: ResourceRef,
  options: KnowledgeOptions<typeof api.importSource>,
  confirmed?: KnowledgeUploadContext,
) {
  const scope = <T extends { path?: object }>(value: T) =>
    scopedKnowledgeOptions(ref, value);
  const unknown = () =>
    new ApiError(
      i18n.t("knowledge:operationOutcomeUnknown", {
        defaultValue: knowledgeZh.operationOutcomeUnknown,
      }),
      "KnowledgeRemoteOutcomeUnknown",
      504,
    );
  const requestId = options.body.requestId;
  // 先核对原登记，挂载和能力改变不能把未知请求变成新的上传。
  const original = await api
    .getRemoteOperation<false>({
      ...scope({ path: { requestId }, signal: options.signal }),
      throwOnError: false,
    })
    .catch(() => undefined);
  if (original?.data) {
    const operation = original.data;
    if (
      operation.requestId !== requestId ||
      operation.ref?.backend !== ref.backend ||
      operation.ref.connectionId !== ref.connectionId ||
      operation.ref.knowledgeBaseId !== ref.knowledgeBaseId ||
      !["source.import", "source.upload.native"].includes(
        operation.operation ?? "",
      )
    )
      throw new ApiError(
        i18n.t("knowledge:operationConflict"),
        "KnowledgeConflict",
        409,
      );
    const result = await api.getRemoteUploadStatus<true>(
      scope({ path: { requestId }, signal: options.signal }),
    );
    if (!result.data.accepted || result.data.transferState !== "accepted")
      throw unknown();
    return result;
  }
  if (original?.response?.status !== 404) throw unknown();
  const capabilities = await getRemoteKnowledgeCapabilities(
    ref,
    options.signal ?? undefined,
  );
  if (
    !confirmed ||
    capabilities.connectionRevision !== confirmed.connectionRevision ||
    capabilities.uploadMode !== confirmed.uploadMode
  )
    throw new ApiError(
      i18n.t("knowledge:uploadModeChanged", {
        defaultValue: knowledgeZh.uploadModeChanged,
      }),
      "KnowledgeUploadModeChanged",
      409,
    );
  remoteUploadContext(capabilities);
  const result = await api.uploadRemoteSource<true>(
    scope({ ...options, body: { ...options.body, ...confirmed } }),
  );
  if (
    result.data.requestId !== requestId ||
    !result.data.accepted ||
    !result.data.sourceId ||
    result.data.transferState !== "accepted" ||
    result.data.uploadMode !== confirmed.uploadMode
  )
    throw unknown();
  return result;
}
