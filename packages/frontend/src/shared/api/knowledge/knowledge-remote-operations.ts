import * as api from "@/shared/api/generated/sdk.gen";
import type {
  GetRemoteCapabilitiesResponse,
  GetRemoteOperationResponse,
} from "@/shared/api/generated/types.gen";
import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import {
  scopedKnowledgeOptions,
  assertKnowledgeCapability,
  type ResourceRef,
  type KnowledgeCapabilities,
  type KnowledgeOptions,
} from "./knowledge-adapter-types";

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

/** 逐操作授权缺少明细时保持不可判定，不能从汇总布尔值猜测权限。 */
function normalizeCapabilities(
  data: GetRemoteCapabilitiesResponse,
  ref: ResourceRef,
): KnowledgeCapabilities {
  const details: KnowledgeCapabilities["details"] = {};
  const source = "details" in data ? data.details : undefined;
  if (record(source))
    for (const [key, value] of Object.entries(source)) {
      if (
        record(value) &&
        typeof value.supported === "boolean" &&
        typeof value.permission === "string" &&
        typeof value.condition === "string"
      )
        details[key] = {
          supported: value.supported,
          permission: value.permission,
          condition: value.condition,
        };
    }
  const operations = { ...data.operations };
  for (const [key, original] of Object.entries({
    settings: "write",
    graph: "read",
  })) {
    if (
      ref.backend !== "integration" &&
      !data.uploadMode &&
      details[original]
    ) {
      details[key] = details[original];
      operations[key] = Boolean(operations[original]);
    }
  }
  return {
    operations,
    details,
    profiles: data.profiles ?? [],
    ...("serviceKind" in data && typeof data.serviceKind === "string"
      ? { serviceKind: data.serviceKind }
      : {}),
    ...("serviceVersion" in data && typeof data.serviceVersion === "string"
      ? { serviceVersion: data.serviceVersion }
      : {}),
    ...("uploadMode" in data &&
    (data.uploadMode === "separate" ||
      data.uploadMode === "automatic" ||
      data.uploadMode === "unknown")
      ? { uploadMode: data.uploadMode }
      : {}),
    ...("connectionRevision" in data &&
    typeof data.connectionRevision === "number"
      ? { connectionRevision: data.connectionRevision }
      : {}),
    ...("evidence" in data &&
    Array.isArray(data.evidence) &&
    data.evidence.every(
      (item: unknown): item is string => typeof item === "string",
    )
      ? { evidence: data.evidence }
      : {}),
  };
}

/** 远端能力与持久回执校验按实际操作加载，不进入本地页面启动链。 */
export async function getRemoteKnowledgeCapabilities(
  ref: ResourceRef,
  signal?: AbortSignal,
): Promise<KnowledgeCapabilities> {
  signal?.throwIfAborted();
  const { data } = await api.getRemoteCapabilities<true>(
    scopedKnowledgeOptions(ref, { path: {}, signal }),
  );
  return normalizeCapabilities(data, ref);
}

/** 新请求必须取得真实能力；原持久操作只允许继续同一请求的回执对账。 */
export const assertRemoteKnowledgeWrite = async (
  ref: ResourceRef,
  key: string,
  requestId: string,
  operation: string,
  signal?: AbortSignal,
) => {
  const scope = <T extends { path?: object }>(options: T) =>
    scopedKnowledgeOptions(ref, options);
  try {
    assertKnowledgeCapability(
      await getRemoteKnowledgeCapabilities(ref, signal),
      key,
    );
  } catch (cause) {
    signal?.throwIfAborted();
    const result = await api
      .getRemoteOperation<false>({
        ...scope({ path: { requestId }, signal }),
        throwOnError: false,
      })
      .catch(() => undefined);
    signal?.throwIfAborted();
    if (result?.response?.status === 404) throw cause;
    if (!result?.data)
      throw new ApiError(
        i18n.t("knowledge:operationOutcomeUnknown", {
          defaultValue: knowledgeZh.operationOutcomeUnknown,
        }),
        "KnowledgeRemoteOutcomeUnknown",
        504,
      );
    const saved = result.data;
    if (
      saved.requestId !== requestId ||
      saved.operation !== operation ||
      saved.ref?.backend !== ref.backend ||
      saved.ref.connectionId !== ref.connectionId ||
      saved.ref.knowledgeBaseId !== ref.knowledgeBaseId
    )
      throw new ApiError(
        i18n.t("knowledge:operationConflict", {
          defaultValue: knowledgeZh.operationConflict,
        }),
        "KnowledgeConflict",
        409,
      );
  }
};

/** 库删除后的能力接口可以消失，恢复仍由持久身份和实际授权约束。 */
export async function recoverRemoteKnowledgeDeletion(
  ref: ResourceRef,
  options: KnowledgeOptions<typeof api.recoverLibraryDeletion>,
) {
  options.signal?.throwIfAborted();
  const scope = <T extends { path?: object }>(value: T) =>
    scopedKnowledgeOptions(ref, value);
  let original: GetRemoteOperationResponse | undefined;
  let knownMissing = false;
  const uncertain = () =>
    new ApiError(
      i18n.t("knowledge:operationOutcomeUnknown", {
        defaultValue: knowledgeZh.operationOutcomeUnknown,
      }),
      "KnowledgeRemoteOutcomeUnknown",
      504,
    );
  try {
    const lookup = await api.getRemoteOperation<false>({
      ...scope({
        path: { requestId: options.body.requestId },
        signal: options.signal,
      }),
      throwOnError: false,
    });
    original = lookup.data;
    knownMissing = lookup.response?.status === 404;
  } catch {
    // 对账暂不可用时仍可核对持久清单，但不能从404推断清理授权。
  }
  if (original) {
    if (
      original.requestId !== options.body.requestId ||
      original.operation !== "library.delete.recover" ||
      original.ref?.backend !== ref.backend ||
      original.ref.connectionId !== ref.connectionId ||
      original.ref.knowledgeBaseId !== ref.knowledgeBaseId
    )
      throw new ApiError(
        i18n.t("knowledge:operationConflict", {
          defaultValue: knowledgeZh.operationConflict,
        }),
        "KnowledgeConflict",
        409,
      );
  } else {
    // 库已逻辑删除后能力接口可以404；以已认证清单证明此次恢复的确切对象与版本。
    const { data: manifest } = await api
      .getRemoteLibraryDeletion<true>(
        scope({ path: {}, signal: options.signal }),
      )
      .catch((cause: unknown) => {
        if (!knownMissing) throw uncertain();
        throw cause;
      });
    if (
      manifest.libraryId !== ref.knowledgeBaseId ||
      manifest.version !== options.body.expectedVersion ||
      !manifest.version ||
      !manifest.policyVersion ||
      !["pending", "blocked", "completed"].includes(manifest.state ?? "")
    ) {
      if (!knownMissing) throw uncertain();
      throw new ApiError(
        i18n.t("knowledge:operationConflict", {
          defaultValue: knowledgeZh.operationConflict,
        }),
        "KnowledgeConflict",
        409,
      );
    }
  }
  return api.recoverRemoteLibraryDeletion<true>(scope(options));
}
