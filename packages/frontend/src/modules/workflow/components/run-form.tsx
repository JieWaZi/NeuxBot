import { useMutation } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router-dom";
import { createPortal } from "react-dom";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { startWorkflowRunMutation } from "../api/workflow-queries";
import {
  jsonText,
  type ExecutionDefaults,
  type WorkflowRevision,
} from "../model/types";
import { SchemaForm, useSchemaFormValidity } from "./schema-form";
import {
  lastRunInputs,
  rememberRunInputs,
} from "../model/run-input-preference";

/** 表单状态独立于画布浮层的挂载，关闭后再次打开仍保留未提交输入。 */
export function RunForm({
  id,
  revision,
  defaults,
  container,
  onClose,
}: {
  id: string;
  revision: WorkflowRevision;
  defaults?: ExecutionDefaults;
  container?: HTMLElement | null;
  onClose: () => void;
}) {
  const { t } = useTranslation("workflow");
  const formId = useId();
  const navigate = useNavigate();
  const location = useLocation();
  const schema = revision.manifest?.inputSchema ?? {
    type: "object",
    properties: {},
  };
  const [args, setArgs] = useState<unknown>(() => lastRunInputs(id, schema));
  const start = useMutation(withLocalErrorMutation(startWorkflowRunMutation()));
  // 源码导入的工作流可能没有创作会话，运行配置直接复用共享目录解析。
  const savedConfiguration = Boolean(defaults?.runtimeId);
  const needsRuntimeDefault = revision.manifest?.nodes.some(
    (node) => node.kind === "worker" && !node.execution.resolve.runtimeId,
  );
  const resolveConfiguration = !savedConfiguration && needsRuntimeDefault;
  const execution = useExecutionSelection({
    value: defaults ?? {},
    defaults: true,
    enabled: Boolean(container && resolveConfiguration),
  });
  const runDefaults = !resolveConfiguration
    ? (defaults ?? {})
    : execution.valid
      ? { ...defaults, ...execution.selection }
      : undefined;
  const configurationUnavailable =
    resolveConfiguration && !execution.loading && !execution.valid;
  const validity = useSchemaFormValidity();
  const [request, setRequest] = useState<{ content: string; id: string }>();
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!validity.valid || start.isPending || !runDefaults) return;
    const content = jsonText({
      args,
      defaults: runDefaults,
      revisionId: revision.id,
    });
    const requestId =
      request?.content === content ? request.id : crypto.randomUUID();
    setRequest({ content, id: requestId });
    try {
      const run = await start.mutateAsync({
        path: { id },
        body: {
          requestId,
          revisionId: revision.id,
          args,
          defaults: runDefaults,
        },
      });
      if (run.id) {
        rememberRunInputs(id, schema, args);
        setRequest(undefined);
        onClose();
        navigate(`/workflows/${id}/runs/${run.id}`, { state: location.state });
      }
    } catch {
      // 请求失败时保留表单与局部错误。
    }
  };
  return container
    ? createPortal(
        <>
          <p className="text-content-secondary type-body px-6 pb-4">
            {t("startHelp")}
          </p>
          <form
            id={formId}
            aria-label={t("inputNodeTitle")}
            onSubmit={(event) => void submit(event)}
            className="grid min-h-0 gap-5 overflow-y-auto overscroll-contain px-6 pb-4"
          >
            <SchemaForm
              fileWorkflowId={id}
              compact
              fieldOrder={revision.manifest?.inputPropertyOrder}
              schema={schema}
              value={args}
              onChange={setArgs}
              onValidityChange={validity.onValidityChange}
            />
            {configurationUnavailable ? (
              <p role="alert" className="text-destructive type-body">
                {execution.error
                  ? localApiErrorMessage(
                      execution.error,
                      t("runConfigurationUnavailable"),
                    )
                  : t("runConfigurationUnavailable")}
              </p>
            ) : null}
            {start.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(start.error, t("startError"))}
              </p>
            ) : null}
          </form>
          <DialogFooter className="border-t-0 py-4">
            <Button
              type="submit"
              form={formId}
              disabled={start.isPending || !validity.valid || !runDefaults}
            >
              {t("run")}
            </Button>
          </DialogFooter>
        </>,
        container,
      )
    : null;
}
