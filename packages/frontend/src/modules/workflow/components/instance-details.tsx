import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import {
  ChatApprovalCard,
  ChatElicitationCard,
  ReadOnlyMarkdown,
} from "@/modules/chat";
import {
  resolveApprovalMutation,
  resolveElicitationMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { WorkerReference } from "./worker-reference";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { getWorkflowInstanceOptions } from "../api/workflow-queries";
import { jsonText } from "../model/types";
import { WorkflowArtifacts } from "./workflow-artifacts";
import { SkipWorkerControl } from "./recovery-controls";
import { record } from "../model/types";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";
import { WorkflowFailure } from "./failure-details";

export function WorkflowResult({ value }: { value: unknown }) {
  const { t } = useTranslation("workflow");
  if (value == null)
    return <p className="text-muted-foreground type-body">{t("emptyValue")}</p>;
  return typeof value === "string" && value.length <= 100_000 ? (
    <ReadOnlyMarkdown source={value} />
  ) : (
    <pre className="bg-muted type-body overflow-auto rounded-lg p-4 whitespace-pre-wrap">
      {jsonText(value)}
    </pre>
  );
}

export function InstanceDetails({
  id,
  runId,
  instanceId,
}: {
  id: string;
  runId: string;
  instanceId: string;
}) {
  const { t, i18n } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const query = useQuery({
    ...getWorkflowInstanceOptions({ path: { id, runId, instanceId } }),
    enabled: Boolean(instanceId),
  });

  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id, parentId: runId },
    queryKeys: [
      getWorkflowInstanceOptions({ path: { id, runId, instanceId } }).queryKey,
    ],
    active,
    enabled: Boolean(id && runId),
  });
  const workerChatId =
    query.data?.chat?.chat?.id || query.data?.instance?.chatId || "";
  useQueryEvents({
    topic: "chat",
    resource: { kind: "chat", id: workerChatId },
    queryKeys: [
      getWorkflowInstanceOptions({ path: { id, runId, instanceId } }).queryKey,
    ],
    active,
    enabled: Boolean(workerChatId),
    eventTypes: ["execution.", "approval.", "elicitation."],
  });
  const resolveApproval = useMutation(
    withLocalErrorMutation(resolveApprovalMutation()),
  );
  const resolveElicitation = useMutation(
    withLocalErrorMutation(resolveElicitationMutation()),
  );
  const data = query.data;
  const instance = data?.instance;
  const chat = data?.chat?.chat;
  const currentExecution = Boolean(
    instance?.executionId && chat?.activeExecutionId === instance.executionId,
  );
  const approval = currentExecution ? chat?.pendingApprovals?.[0] : undefined;
  const elicitation = currentExecution
    ? chat?.pendingElicitations?.[0]
    : undefined;
  const error =
    resolveApproval.error ?? resolveElicitation.error ?? query.error;
  const refresh = () => void query.refetch();
  return (
    <section className="grid min-w-0 gap-4">
      {query.isPending ? <WorkflowLoadingState /> : null}
      {query.isError ? (
        <WorkflowQueryError
          error={query.error}
          retrying={query.isFetching}
          onRetry={refresh}
        />
      ) : null}
      {instance?.chatId ? (
        <div className="flex flex-wrap items-center gap-3">
          <Link
            to={`/chats/${instance.chatId}`}
            className="text-link type-body focus-visible:ring-ring rounded-sm underline underline-offset-2 focus-visible:ring-2 focus-visible:outline-none"
          >
            {t("openChat")}
          </Link>
        </div>
      ) : null}
      {instance ? (
        <div className="text-muted-foreground type-caption flex flex-wrap items-center gap-x-3 gap-y-2">
          {instance.kind === "worker" ? (
            <WorkerReference
              showModel={false}
              resolve={
                chat?.wakerId
                  ? { wakerId: chat.wakerId, model: data?.execution?.model }
                  : record(instance.options)?.resolve
              }
            />
          ) : null}
          {instance.createdAt ? (
            <time dateTime={new Date(instance.createdAt).toISOString()}>
              {new Date(instance.createdAt).toLocaleString(i18n.language)}
            </time>
          ) : null}
          {instance.createdAt && instance.completedAt ? (
            <span>
              {t("duration", {
                seconds: (
                  (instance.completedAt - instance.createdAt) /
                  1000
                ).toFixed(1),
              })}
            </span>
          ) : null}
          {data?.execution ? (
            <span>
              {data.execution.runtimeId} · {data.execution.model}
            </span>
          ) : null}
        </div>
      ) : null}
      {error && !query.isError ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(error, t("error"))}
        </p>
      ) : null}
      {instance?.id &&
      data?.effect &&
      instance.kind === "worker" &&
      record(instance.options)?.skippable === true &&
      !instance.completedAt &&
      data.effect.status !== "unknown" ? (
        <SkipWorkerControl
          id={id}
          runId={runId}
          instanceId={instance.id}
          generation={data.effect.attemptGeneration ?? 0}
          skipRequested={data.effect.skipRequested ?? false}
          onChanged={refresh}
        />
      ) : null}
      {data?.effect?.status === "unknown" && !instance?.failure ? (
        <p role="alert" className="text-destructive type-body">
          {t("uncertainFailureHelp")}
        </p>
      ) : null}
      {active && !instance?.completedAt && approval && chat?.id ? (
        <div className="bg-surface-module rounded-lg">
          <ChatApprovalCard
            approval={approval}
            current={1}
            total={chat.pendingApprovals?.length ?? 1}
            submissionState={resolveApproval.isPending ? "submitting" : "idle"}
            onResolve={(approvalId, optionId) =>
              resolveApproval.mutate(
                {
                  path: { chatId: chat.id ?? "", approvalId },
                  body: { optionId },
                },
                { onSuccess: refresh },
              )
            }
          />
        </div>
      ) : null}
      {active && !instance?.completedAt && elicitation && chat?.id ? (
        <div className="bg-surface-module overflow-hidden rounded-lg">
          <ChatElicitationCard
            elicitation={elicitation}
            submissionState={
              resolveElicitation.isPending ? "submitting" : "idle"
            }
            onResolve={(elicitationId, content, declined) =>
              resolveElicitation.mutate(
                {
                  path: { chatId: chat.id ?? "", elicitationId },
                  body: { content, declined },
                },
                { onSuccess: refresh },
              )
            }
          />
        </div>
      ) : null}
      {instance ? (
        <>
          {instance.input?.length &&
          (instance.kind !== "askUser" || instance.completedAt) ? (
            <section className="grid gap-3">
              <h3 className="text-muted-foreground type-label font-medium">
                {t(
                  instance.kind === "worker" || instance.kind === "askUser"
                    ? "instruction"
                    : "stepInputs",
                )}
              </h3>
              <div className="bg-card max-h-80 overflow-y-auto rounded-lg p-4">
                <WorkflowResult
                  value={
                    instance.kind === "worker" ||
                    instance.kind === "askUser" ||
                    instance.input?.length === 1
                      ? instance.input?.[0]?.data
                      : instance.input?.map((item) => item.data)
                  }
                />
              </div>
            </section>
          ) : null}
          {instance.completedAt ||
          instance.failure ||
          instance.output != null ? (
            <div className="grid gap-3">
              <h3 className="type-label font-medium">
                {instance.failure ? t("failure") : t("output")}
              </h3>
              <div className="bg-card max-h-80 overflow-y-auto rounded-lg p-4">
                {instance.failure ? (
                  <WorkflowFailure value={instance.failure.data} />
                ) : (
                  <WorkflowResult value={instance.output} />
                )}
              </div>
            </div>
          ) : null}
        </>
      ) : null}
      {data?.actionCalls?.length ? (
        <section className="grid gap-3">
          <h3 className="type-label font-medium">{t("actionReceipts")}</h3>
          {data.actionCalls.map((call) => (
            <details key={call.id} className="bg-surface-module rounded-lg p-4">
              <summary className="type-body cursor-pointer">
                {call.callId} · {call.toolName} ·{" "}
                {call.status ? t(`status.${call.status}`) : ""}
              </summary>
              <div className="mt-3 grid gap-3">
                <WorkflowResult value={call.input} />
                {call.failure ? (
                  <WorkflowFailure value={call.failure.data} />
                ) : (
                  <WorkflowResult value={call.result} />
                )}
              </div>
            </details>
          ))}
        </section>
      ) : null}
      {(data?.attempts?.length ?? 0) > 1 ? (
        <section className="grid gap-3">
          <h3 className="type-label font-medium">{t("attemptHistory")}</h3>
          {data?.attempts?.map((attempt) => (
            <details
              key={attempt.id}
              className="bg-surface-module rounded-lg p-4"
            >
              <summary className="type-body cursor-pointer">
                {t("attemptNumber", { count: attempt.attemptGeneration })} ·{" "}
                {attempt.status ? t(`status.${attempt.status}`) : ""}
              </summary>
              <div className="mt-3 grid gap-2">
                <p className="text-muted-foreground type-caption break-all">
                  {attempt.executionId}
                </p>
                {attempt.chatId ? (
                  <Link
                    to={`/chats/${attempt.chatId}`}
                    className="text-link type-body focus-visible:ring-ring rounded-sm underline underline-offset-2 focus-visible:ring-2 focus-visible:outline-none"
                  >
                    {t("openChat")}
                  </Link>
                ) : null}
                {attempt.failure ? (
                  <WorkflowFailure value={attempt.failure.data} />
                ) : null}
              </div>
            </details>
          ))}
        </section>
      ) : null}
      {instance ? (
        <WorkflowArtifacts id={id} runId={runId} values={[instance.output]} />
      ) : null}
    </section>
  );
}
