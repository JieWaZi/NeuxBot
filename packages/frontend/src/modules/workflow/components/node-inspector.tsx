import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { listWakersOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { EmptyState } from "@/shared/ui/empty-state";
import {
  record,
  type Manifest,
  type WorkflowNode,
  type WorkflowPhase,
} from "../model/types";
import { ActionDetails } from "./action-details";
import { editablePrompt } from "../model/prompt-editor";
import { useWorkflowConfiguration } from "../model/use-workflow-configuration";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";

export function NodeInspector({
  id,
  revisionId,
  node,
  phase,
  phaseNodes,
  manifest,
  onNode,
  readOnly = false,
}: {
  id: string;
  revisionId: string;
  node?: WorkflowNode;
  phase?: WorkflowPhase;
  phaseNodes?: WorkflowNode[];
  manifest?: Manifest;
  onNode?: (node: WorkflowNode) => void;
  readOnly?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const fieldId = useId();
  const options = record(node?.options) ?? {};
  const resolve = record(options.resolve) ?? {};
  const originalLabel = node?.labelTemplate ?? phase?.title ?? "";
  const originalPrompt = editablePrompt(node?.promptTemplate ?? "");
  const originalDetail = phase?.detail ?? "";
  const originalCode = typeof options.code === "string" ? options.code : "";
  const [label, setLabel] = useState(originalLabel);
  const [prompt, setPrompt] = useState(originalPrompt);
  const [detail, setDetail] = useState(originalDetail);
  const [code, setCode] = useState(originalCode);
  const originalSelection: ExecutionSelection = {
    runtimeId:
      typeof resolve.runtimeId === "string" ? resolve.runtimeId : undefined,
    accountId:
      typeof resolve.accountId === "string" ? resolve.accountId : undefined,
    model: typeof resolve.model === "string" ? resolve.model : undefined,
    reasoning:
      typeof resolve.reasoning === "string" ? resolve.reasoning : undefined,
    permissionLevel:
      typeof resolve.permissionLevel === "string"
        ? resolve.permissionLevel
        : undefined,
  };
  const [selection, setSelection] = useState(originalSelection);
  const target = node ? "node" : "phase";
  const targetId = node?.id ?? phase?.id;
  const [savedFields, setSavedFields] = useState({
    definitionId: id,
    target,
    targetId,
    revisionId,
    label: originalLabel,
    prompt: originalPrompt,
    detail: originalDetail,
    code: originalCode,
    selection: originalSelection,
  });
  const identityChanged =
    savedFields.definitionId !== id ||
    savedFields.target !== target ||
    savedFields.targetId !== targetId;
  // 同一目标的远端版本只同步未编辑项；保留草稿，提交仍使用最新 revision。
  if (identityChanged || savedFields.revisionId !== revisionId) {
    setSavedFields({
      definitionId: id,
      target,
      targetId,
      revisionId,
      label: originalLabel,
      prompt: originalPrompt,
      detail: originalDetail,
      code: originalCode,
      selection: originalSelection,
    });
    if (identityChanged || label === savedFields.label) setLabel(originalLabel);
    if (identityChanged || prompt === savedFields.prompt)
      setPrompt(originalPrompt);
    if (identityChanged || detail === savedFields.detail)
      setDetail(originalDetail);
    if (identityChanged || code === savedFields.code) setCode(originalCode);
    const synchronizedSelection = (field: keyof ExecutionSelection) =>
      selection[field] === savedFields.selection[field]
        ? originalSelection[field]
        : selection[field];
    setSelection(
      identityChanged
        ? originalSelection
        : {
            ...selection,
            runtimeId: synchronizedSelection("runtimeId"),
            accountId: synchronizedSelection("accountId"),
            model: synchronizedSelection("model"),
            reasoning: synchronizedSelection("reasoning"),
            permissionLevel: synchronizedSelection("permissionLevel"),
          },
    );
  }
  const execution = useExecutionSelection({
    value: selection,
    enabled: node?.kind === "worker",
  });
  const wakerId = typeof resolve.wakerId === "string" ? resolve.wakerId : "";
  const wakers = useQuery({
    ...listWakersOptions({ query: { pageNum: 1, pageSize: 100 } }),
    enabled: node?.kind === "worker",
  });
  const configure = useWorkflowConfiguration(id, revisionId);
  const disabled = readOnly || configure.pending;
  const commit = (fields: Record<string, unknown>, changedPrompt?: string) =>
    configure.save({
      target,
      id: targetId,
      fields,
      prompt: changedPrompt,
    });
  if (!node && !phase)
    return (
      <EmptyState title={t("nodeDetails")} description={t("selectedEmpty")} />
    );
  return (
    <div className="grid content-start gap-5 p-6 pt-2">
      <div className="grid gap-2">
        <FieldLabel htmlFor={`${fieldId}-title`}>
          {node ? t("nodeTitle") : t("phaseTitle")}
        </FieldLabel>
        <Input
          id={`${fieldId}-title`}
          value={label}
          placeholder={node ? t("nodeTitle") : t("phaseTitle")}
          disabled={disabled}
          onChange={(event) => setLabel(event.target.value)}
          onBlur={() => {
            if (label !== originalLabel && label.trim())
              void commit({ [node ? "label" : "title"]: label });
          }}
        />
      </div>
      {node?.kind === "worker" ? (
        <>
          <div className="grid gap-2">
            <FieldLabel htmlFor={`${fieldId}-partner`}>
              {t("assignedPartner")}
            </FieldLabel>
            <Select
              value={wakerId}
              disabled={disabled || !wakers.data}
              onValueChange={(next) => {
                if (next === wakerId) return;
                const policy = record(options.toolPolicy) ?? {};
                void commit({
                  resolve: { ...resolve, wakerId: next },
                  toolPolicy: { ...policy, connectors: { mode: "inherit" } },
                });
              }}
            >
              <SelectTrigger
                id={`${fieldId}-partner`}
                aria-label={t("assignedPartner")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(wakers.data?.resources ?? []).flatMap((waker) =>
                  waker.id
                    ? [
                        <SelectItem key={waker.id} value={waker.id}>
                          <span className="flex items-center gap-2">
                            <Avatar
                              name={waker.name ?? ""}
                              src={waker.avatar}
                              size="xs"
                              animated={false}
                              interactive={false}
                            />
                            <span>{waker.name}</span>
                          </span>
                        </SelectItem>,
                      ]
                    : [],
                )}
              </SelectContent>
            </Select>
            {wakers.isPending ? <WorkflowLoadingState /> : null}
            {wakers.isError ? (
              <WorkflowQueryError
                error={wakers.error}
                retrying={wakers.isFetching}
                onRetry={() => void wakers.refetch()}
              />
            ) : wakers.isSuccess && !wakers.data.resources?.length ? (
              <EmptyState
                density="compact"
                title={t("common:partnerNavigation.noPartner")}
              />
            ) : null}
          </div>
          <div className="grid gap-2">
            <FieldLabel>{t("configuration")}</FieldLabel>
            <div className="flex flex-wrap gap-2">
              <ExecutionConfigurationControls
                state={execution}
                disabled={disabled}
                onChange={(next) => {
                  setSelection(next);
                  void commit({
                    resolve: {
                      ...resolve,
                      runtimeId: next.runtimeId,
                      accountId: next.accountId,
                      model: next.model,
                      reasoning: next.reasoning,
                      permissionLevel: next.permissionLevel,
                    },
                  });
                }}
              />
            </div>
          </div>
        </>
      ) : null}
      {node?.kind === "worker" || node?.kind === "askUser" ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${fieldId}-prompt`}>{t("prompt")}</FieldLabel>
          <Textarea
            id={`${fieldId}-prompt`}
            value={prompt}
            placeholder={t("prompt")}
            disabled={disabled}
            className="min-h-48"
            onChange={(event) => setPrompt(event.target.value)}
            onBlur={() => {
              if (prompt !== editablePrompt(node.promptTemplate ?? ""))
                void commit({}, prompt);
            }}
          />
        </div>
      ) : null}
      {phase ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${fieldId}-detail`}>
            {t("description")}
          </FieldLabel>
          <Textarea
            id={`${fieldId}-detail`}
            value={detail}
            placeholder={t("description")}
            disabled={disabled}
            className="min-h-40"
            onChange={(event) => setDetail(event.target.value)}
            onBlur={() => {
              if (detail !== phase.detail && detail.trim())
                void commit({ detail });
            }}
          />
        </div>
      ) : null}
      {phase && phaseNodes?.length ? (
        <section className="grid gap-2">
          <h3 className="type-label font-medium">{t("phaseNodes")}</h3>
          {phaseNodes.map((item) => (
            <Button
              key={item.id}
              variant="surface"
              className="justify-start"
              onClick={() => onNode?.(item)}
            >
              {item.labelTemplate ||
                t(item.kind === "worker" ? "assignedPartner" : "nodeDetails")}
            </Button>
          ))}
        </section>
      ) : null}
      {node?.kind === "functionTask" ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${fieldId}-code`}>
            {t("functionCode")}
          </FieldLabel>
          <Textarea
            id={`${fieldId}-code`}
            value={code}
            placeholder={t("functionCode")}
            disabled={disabled}
            className="min-h-64"
            onChange={(event) => setCode(event.target.value)}
            onBlur={() => {
              if (code !== options.code) void commit({ code });
            }}
          />
        </div>
      ) : null}
      {node?.kind === "action" && manifest ? (
        <ActionDetails
          id={id}
          revisionId={revisionId}
          node={node}
          manifest={manifest}
        />
      ) : null}
      {configure.pending ? (
        <p role="status" className="text-muted-foreground type-caption">
          {t("saving")}
        </p>
      ) : null}
      {configure.error ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(configure.error, t("error"))}
        </p>
      ) : null}
    </div>
  );
}
