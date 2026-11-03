import { useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { Button } from "@/shared/ui/button";
import {
  type Manifest,
  type WorkflowNode,
  type WorkflowPhase,
} from "../model/types";
import { editablePrompt } from "../model/prompt-editor";
import { useWorkflowConfiguration } from "../model/use-workflow-configuration";
import { WorkerReference } from "./worker-reference";
import { NodeKind } from "./workflow-canvas";

/** 详情只表达业务职责、真实伙伴、操作风险和用户问题；执行协议留在Core。 */
export function NodeInspector({
  id,
  revisionId,
  node,
  phase,
  phaseNodes,
  onNode,
  manifest,
  readOnly = false,
}: {
  id: string;
  revisionId: string;
  node?: WorkflowNode;
  phase?: WorkflowPhase;
  phaseNodes?: WorkflowNode[];
  onNode?: (node: WorkflowNode) => void;
  manifest?: Manifest;
  readOnly?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const title = node?.label ?? phase?.title ?? "";
  const originalPrompt =
    node?.kind === "worker" || node?.kind === "askUser"
      ? editablePrompt(node.execution.promptTemplate ?? "")
      : "";
  const [label, setLabel] = useState(title);
  const [prompt, setPrompt] = useState(originalPrompt);
  const [baseline, setBaseline] = useState({
    revisionId,
    title,
    prompt: originalPrompt,
  });
  if (baseline.revisionId !== revisionId) {
    if (label === baseline.title) setLabel(title);
    if (prompt === baseline.prompt) setPrompt(originalPrompt);
    setBaseline({ revisionId, title, prompt: originalPrompt });
  }
  const configure = useWorkflowConfiguration(id, revisionId);
  const action =
    node?.kind === "action"
      ? manifest?.actions?.find(
          (action) => action.id === node.execution.actionId,
        )
      : undefined;
  const description = node?.description || action?.description || phase?.detail;
  const risk = action?.risk;
  const riskLabel =
    risk === "low"
      ? t("riskLow")
      : risk === "medium"
        ? t("riskMedium")
        : risk === "high"
          ? t("riskHigh")
          : risk === "read"
            ? t("readRisk")
            : risk === "write"
              ? t("writeRisk")
              : undefined;
  const save = (fields: Record<string, unknown>, changedPrompt?: string) =>
    void configure.save({
      target: node ? "node" : "phase",
      id: node?.id ?? phase?.id,
      fields,
      prompt: changedPrompt,
    });
  return (
    <section className="grid min-w-0 gap-4 px-6 pb-6">
      <div className="grid gap-2">
        <FieldLabel htmlFor={`workflow-label-${node?.id ?? phase?.id}`}>
          {t("nodeTitle")}
        </FieldLabel>
        <Input
          id={`workflow-label-${node?.id ?? phase?.id}`}
          value={label}
          placeholder={title}
          readOnly={readOnly}
          disabled={configure.pending}
          onChange={(event) => setLabel(event.target.value)}
          onBlur={() => {
            if (label.trim() && label !== title)
              save(node ? { label } : { title: label });
          }}
        />
      </div>
      {description ? (
        <p className="text-content-secondary type-body whitespace-pre-wrap">
          {description}
        </p>
      ) : null}
      {node?.kind === "worker" ? (
        <WorkerReference resolve={node.execution.resolve} showModel={false} />
      ) : node ? (
        <NodeKind
          kind={node.kind}
          actionType={
            node.kind === "action" ? node.execution.actionType : undefined
          }
          label={action?.type === "script" ? t("localScript") : undefined}
        />
      ) : null}
      {riskLabel ? (
        <p className="type-body">
          {t("option.risk")} · {riskLabel}
        </p>
      ) : null}
      {node?.kind === "worker" || node?.kind === "askUser" ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`workflow-prompt-${node.id}`}>
            {t("prompt")}
          </FieldLabel>
          <Textarea
            id={`workflow-prompt-${node.id}`}
            value={prompt}
            placeholder={t("prompt")}
            readOnly={readOnly}
            disabled={configure.pending}
            onChange={(event) => setPrompt(event.target.value)}
            onBlur={() => {
              if (prompt !== originalPrompt) save({}, prompt);
            }}
          />
        </div>
      ) : null}
      {phaseNodes?.map((item) => (
        <Button key={item.id} variant="surface" onClick={() => onNode?.(item)}>
          {item.label}
        </Button>
      ))}
      {configure.error ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(configure.error, t("error"))}
        </p>
      ) : null}
    </section>
  );
}
