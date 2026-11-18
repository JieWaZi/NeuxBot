import { useId, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import {
  record,
  type ExecutionDefaults,
  type Manifest,
  type WorkflowNode,
  type WorkflowPhase,
} from "../model/types";
import { editablePrompt } from "../model/prompt-editor";
import { useWorkflowConfiguration } from "../model/use-workflow-configuration";
import { WorkerReference } from "./worker-reference";
import { NodeKind } from "./workflow-canvas";
import { WorkerConfigurationFields } from "./worker-configuration-fields";

function sameResolve(
  left: Record<string, unknown>,
  right: Record<string, unknown>,
) {
  return [...new Set([...Object.keys(left), ...Object.keys(right)])].every(
    (key) => left[key] === right[key],
  );
}

/** 创作节点共用一次显式配置保存；历史版本仅展示冻结配置。 */
export function NodeInspector({
  id,
  revisionId,
  node,
  phase,
  phaseNodes,
  onNode,
  manifest,
  defaults,
  readOnly = false,
}: {
  id: string;
  revisionId: string;
  node?: WorkflowNode;
  phase?: WorkflowPhase;
  phaseNodes?: WorkflowNode[];
  onNode?: (node: WorkflowNode) => void;
  manifest?: Manifest;
  defaults?: ExecutionDefaults;
  readOnly?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const formId = useId();
  const title = node?.label ?? phase?.title ?? "";
  const originalPrompt =
    node?.kind === "worker" || node?.kind === "askUser"
      ? editablePrompt(node.execution.promptTemplate ?? "")
      : "";
  const original = {
    label: title,
    prompt: originalPrompt,
    resolve:
      node?.kind === "worker" ? (record(node.execution.resolve) ?? {}) : {},
  };
  const identity = JSON.stringify([
    id,
    node ? "node" : "phase",
    node?.id ?? phase?.id,
    readOnly ? revisionId : null,
  ]);
  const [draft, setDraft] = useState(original);
  const [baseline, setBaseline] = useState({
    revisionId,
    identity,
    values: original,
  });
  const [saved, setSaved] = useState(false);
  const context = useRef({ identity, baseline, generation: 0 });
  useLayoutEffect(() => {
    context.current = {
      identity,
      baseline,
      generation:
        context.current.generation +
        (context.current.identity === identity ? 0 : 1),
    };
  }, [identity, baseline]);
  if (baseline.identity !== identity || baseline.revisionId !== revisionId) {
    const changedTarget = baseline.identity !== identity;
    setDraft({
      label:
        changedTarget || draft.label === baseline.values.label
          ? original.label
          : draft.label,
      prompt:
        changedTarget || draft.prompt === baseline.values.prompt
          ? original.prompt
          : draft.prompt,
      // 只保留实际编辑的键，后台修订的其他执行字段继续随权威版本更新。
      resolve: changedTarget
        ? original.resolve
        : Object.fromEntries(
            [
              ...new Set([
                ...Object.keys(original.resolve),
                ...Object.keys(draft.resolve),
              ]),
            ].map((key) => [
              key,
              draft.resolve[key] === baseline.values.resolve[key]
                ? original.resolve[key]
                : draft.resolve[key],
            ]),
          ),
    });
    setBaseline({ revisionId, identity, values: original });
    setSaved(false);
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
  const dirty =
    draft.label !== baseline.values.label ||
    draft.prompt !== baseline.values.prompt ||
    !sameResolve(draft.resolve, baseline.values.resolve);
  const change = (patch: Partial<typeof draft>) => {
    setDraft({ ...draft, ...patch });
    setSaved(false);
  };
  const save = async () => {
    if (readOnly || configure.pending || !dirty || !draft.label.trim()) return;
    const submitted = { ...draft, label: draft.label.trim() };
    const editingContext = context.current;
    const fields: Record<string, unknown> = {};
    if (submitted.label !== baseline.values.label)
      fields[node ? "label" : "title"] = submitted.label;
    if (node?.kind === "worker") {
      for (const [key, value] of Object.entries(submitted.resolve)) {
        if (value !== baseline.values.resolve[key])
          fields[`resolve.${key}`] = value;
      }
    }
    if (
      (await configure.save({
        target: node ? "node" : "phase",
        id: node?.id ?? phase?.id,
        fields,
        prompt:
          submitted.prompt !== baseline.values.prompt
            ? submitted.prompt
            : undefined,
      })) &&
      context.current.generation === editingContext.generation &&
      context.current.identity === editingContext.identity
    ) {
      // 保存完成前可能已读到更新版本，确认成功时优先使用最新权威值。
      const values =
        context.current.baseline.revisionId === revisionId
          ? submitted
          : context.current.baseline.values;
      setDraft(values);
      setBaseline((current) => ({ ...current, values }));
      setSaved(true);
    }
  };
  return (
    <form
      id={formId}
      className="contents"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <section className="grid min-h-0 min-w-0 gap-4 overflow-y-auto overscroll-contain px-6 pb-6">
        <div className="grid gap-2">
          <FieldLabel htmlFor={`workflow-label-${node?.id ?? phase?.id}`}>
            {t(node ? "nodeTitle" : "phaseTitle")}
          </FieldLabel>
          <Input
            id={`workflow-label-${node?.id ?? phase?.id}`}
            value={draft.label}
            placeholder={title}
            readOnly={readOnly}
            disabled={configure.pending}
            onChange={(event) => change({ label: event.target.value })}
          />
        </div>
        {description ? (
          <p className="text-content-secondary type-body whitespace-pre-wrap">
            {description}
          </p>
        ) : null}
        {node?.kind === "worker" ? (
          readOnly ? (
            <WorkerReference resolve={node.execution.resolve} />
          ) : (
            <WorkerConfigurationFields
              fieldId={formId}
              defaults={defaults}
              resolve={draft.resolve}
              disabled={configure.pending}
              onChange={(resolve) => change({ resolve })}
            />
          )
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
              value={draft.prompt}
              placeholder={t("prompt")}
              readOnly={readOnly}
              disabled={configure.pending}
              onChange={(event) => change({ prompt: event.target.value })}
            />
          </div>
        ) : null}
        {phaseNodes?.map((item) => (
          <Button
            key={item.id}
            type="button"
            variant="surface"
            onClick={() => onNode?.(item)}
          >
            {item.label}
          </Button>
        ))}
        {configure.error ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(configure.error, t("error"))}
          </p>
        ) : null}
      </section>
      {!readOnly ? (
        <DialogFooter>
          {saved ? (
            <span role="status" className="text-content-secondary type-body">
              {t("configurationSaved")}
            </span>
          ) : null}
          <Button
            type="button"
            variant="secondary"
            disabled={configure.pending || !dirty}
            onClick={() => {
              setDraft(baseline.values);
              setSaved(false);
            }}
          >
            {t("cancel")}
          </Button>
          <Button
            type="submit"
            form={formId}
            disabled={configure.pending || !dirty || !draft.label.trim()}
          >
            {t(configure.pending ? "saving" : "save")}
          </Button>
        </DialogFooter>
      ) : null}
    </form>
  );
}
