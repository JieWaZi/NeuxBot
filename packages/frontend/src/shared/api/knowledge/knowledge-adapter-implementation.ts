import { ApiError } from "@/shared/api/errors";
import { i18n } from "@/shared/i18n";
import { knowledgeZh } from "@/shared/i18n/knowledge";
import { localKnowledgeAdapter } from "./knowledge-local-adapter";
import { remoteKnowledgeAdapter } from "./knowledge-remote-adapter";
import type { ResourceRef } from "./knowledge-adapter-types";
export type { KnowledgeCapabilities } from "./knowledge-adapter-types";

/** 工厂固定资源身份；未知后端一律拒绝，不得落入本机默认分支。 */
export function knowledgeAdapter(input: ResourceRef) {
  const ref = Object.freeze({ ...input });
  if (ref.backend === "local") return localKnowledgeAdapter(ref);
  if (
    (ref.backend === "custom" ||
      ref.backend === "weknora" ||
      ref.backend === "integration") &&
    ref.connectionId &&
    ref.connectionId !== "local"
  )
    return remoteKnowledgeAdapter(ref);
  throw new ApiError(
    i18n.t("knowledge:invalidResource", {
      defaultValue: knowledgeZh.invalidResource,
    }),
    "KnowledgeInvalid",
    400,
  );
}
