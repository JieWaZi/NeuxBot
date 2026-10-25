import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { FieldLabel } from "@/shared/ui/field-label";
import { Textarea } from "@/shared/ui/textarea";
import { getWorkflowSourceOptions } from "../api/workflow-queries";
import type { Manifest, WorkflowNode } from "../model/types";

/** 展示固定版本的真实操作调用和实现，不执行源码或读取外部脚本。 */
export function ActionDetails({
  id,
  revisionId,
  node,
  manifest,
}: {
  id: string;
  revisionId: string;
  node: WorkflowNode;
  manifest: Manifest;
}) {
  const { t } = useTranslation("workflow");
  const fieldId = useId();
  const query = useQuery(
    getWorkflowSourceOptions({ path: { id }, query: { revisionId } }),
  );
  const detail = query.data?.actionDetails?.[node.id ?? ""];
  const action = manifest.meta?.actions?.find(
    (item) => item.id === detail?.actionId,
  );
  if (query.isPending) return <p role="status">{t("actionDetailsLoading")}</p>;
  const errorFeedback = (
    <div className="grid gap-2">
      <p role="alert" className="text-destructive type-body">
        {t("actionDetailsFailed")}
      </p>
      <Button
        size="sm"
        variant="secondary"
        onClick={() => void query.refetch()}
      >
        {t("reload")}
      </Button>
    </div>
  );
  if (!detail || !action) return errorFeedback;
  const target =
    action.type === "script"
      ? {
          executable: action.executable,
          workingDirectory: action.workingDirectory,
        }
      : action.type === "http"
        ? { allowedOrigins: action.allowedOrigins, methods: action.methods }
        : { handler: action.handler, connectorIds: action.connectorIds };
  // 执行目标来自同一版本的声明；用注释补全调用原文，不伪造可执行代码。
  const targetSource = Object.entries({
    id: detail.actionId,
    type: action.type,
    ...target,
  })
    .filter(([, value]) => value !== undefined)
    .map(([name, value]) => `// ${name}: ${JSON.stringify(value)}`)
    .join("\n");
  const callSource = [targetSource, detail.callSource, detail.handlerSource]
    .filter(Boolean)
    .join("\n\n");
  return (
    <section className="grid min-w-0 gap-4" aria-label={t("actionCall")}>
      {query.isError ? errorFeedback : null}
      <div className="grid gap-2">
        <FieldLabel htmlFor={`${fieldId}-call`}>{t("actionCall")}</FieldLabel>
        <Textarea
          id={`${fieldId}-call`}
          value={callSource}
          placeholder={t("actionCall")}
          readOnly
          spellCheck={false}
          className="min-h-48 font-mono"
        />
      </div>
    </section>
  );
}
