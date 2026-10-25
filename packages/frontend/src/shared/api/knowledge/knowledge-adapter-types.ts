import type * as api from "@/shared/api/generated/sdk.gen";
import type { GetRemoteCapabilitiesResponse } from "@/shared/api/generated/types.gen";
import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
export type ResourceRef = {
  backend: string;
  connectionId: string;
  knowledgeBaseId: string;
};

export type KnowledgeOptions<F extends (...args: never[]) => unknown> =
  NonNullable<Parameters<F>[0]>;
export type KnowledgeUploadContext = {
  uploadMode: "separate" | "automatic";
  connectionRevision: number;
};
export type KnowledgeCapabilities = {
  serviceKind?: string;
  serviceVersion?: string;
  uploadMode?: "separate" | "automatic" | "unknown";
  connectionRevision?: number;
  evidence?: string[];
  operations: Record<string, boolean>;
  details: Record<
    string,
    { supported: boolean; permission: string; condition: string }
  >;
  profiles: NonNullable<GetRemoteCapabilitiesResponse["profiles"]>;
};
export type KnowledgePageReadOptions = Omit<
  KnowledgeOptions<typeof api.getPage>,
  "query"
> & {
  query?: { slug?: string };
};
export type KnowledgeRevisionsOptions = Omit<
  KnowledgeOptions<typeof api.listRevisions>,
  "query"
> & {
  query?: { offset?: number; limit?: number; slug?: string };
};
export type KnowledgeOrganizeOptions =
  | KnowledgeOptions<typeof api.organize>
  | KnowledgeOptions<typeof api.organizeRemote>;

/** 实例身份覆盖路径；保留调用方错误呈现声明，由 Client 运行时剥离内部标记。 */
export function scopedKnowledgeOptions<T extends { path?: object }>(
  ref: ResourceRef,
  options: T,
) {
  const headers = new Headers(
    (options as T & { headers?: HeadersInit }).headers,
  );
  return {
    ...options,
    headers: Object.fromEntries(headers.entries()),
    path: {
      ...options.path,
      libraryId: ref.knowledgeBaseId,
      connectionId: ref.connectionId,
    },
    throwOnError: true as const,
  };
}

export function knowledgeUnsupported(): never {
  throw new ApiError(
    i18n.t("knowledge:remoteUnsupported", {
      defaultValue: knowledgeZh.remoteUnsupported,
    }),
    "KnowledgeUnsupported",
    501,
  );
}
export function knowledgeDirectoryIncomplete(): never {
  throw new ApiError(
    i18n.t("knowledge:incompleteDirectory", {
      defaultValue: knowledgeZh.incompleteDirectory,
    }),
    "KnowledgeIncompleteDirectory",
    502,
  );
}
/** 静态支持、已知授权与配置条件必须同时满足，不能只读汇总布尔值。 */
export function hasKnowledgeCapability(
  capabilities: KnowledgeCapabilities | undefined,
  key: string,
) {
  const status = capabilities?.details[key];
  return Boolean(
    status?.supported &&
    status.permission === "allowed" &&
    status.condition === "ready" &&
    capabilities?.operations[key],
  );
}

export function canUploadKnowledge(
  capabilities: KnowledgeCapabilities | undefined,
) {
  if (!capabilities) return false;
  if (capabilities.uploadMode === "unknown") return false;
  if (capabilities.uploadMode === "automatic") {
    const status = capabilities.details.upload;
    return Boolean(
      capabilities.connectionRevision &&
      status?.supported &&
      status.permission !== "denied" &&
      ["ready", "automatic_processing"].includes(status.condition),
    );
  }
  return hasKnowledgeCapability(capabilities, "rawOnly");
}

export function assertKnowledgeCapability(
  capabilities: KnowledgeCapabilities,
  key: string,
) {
  const status = capabilities.details[key];
  if (!status?.supported) knowledgeUnsupported();
  if (status.permission !== "allowed")
    throw new ApiError(
      i18n.t("knowledge:permissionDenied", {
        defaultValue: knowledgeZh.permissionDenied,
      }),
      "KnowledgeForbidden",
      403,
    );
  if (status.condition !== "ready" || !capabilities.operations[key])
    throw new ApiError(
      i18n.t("knowledge:conditionUnavailable", {
        defaultValue: knowledgeZh.conditionUnavailable,
      }),
      "KnowledgeConditionUnavailable",
      409,
    );
}
