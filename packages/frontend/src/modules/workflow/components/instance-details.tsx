import { humanAnswerLabel } from "../model/human-answer";
import type { HumanRequest } from "../model/types";
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
  getChatOptions,
  getChatQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { WorkerReference } from "./worker-reference";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  getWorkflowInstanceOptions,
  getWorkflowInstanceQueryKey,
} from "../api/workflow-queries";
import { SkipWorkerControl } from "./recovery-controls";
import { record } from "../model/types";
import {
  WorkflowLoadingState,
  WorkflowQueryError,
  WorkflowRequestLoadingState,
} from "./query-feedback";
import { WorkflowFailure } from "./failure-details";
import { RunJsonValue } from "./run-json-value";

export function WorkflowResult({
  value,
  schema,
}: {
  value: unknown;
  schema?: Record<string, unknown>;
}) {
  const { t } = useTranslation("workflow");
  if (value == null)
    return <p className="text-muted-foreground type-body">{t("emptyValue")}</p>;
  if (typeof value === "string") return <ReadOnlyMarkdown source={value} />;
  if (Array.isArray(value))
    return (
      <ul className="grid gap-2">
        {value.map((item, index) => (
          <li key={index}>
            <WorkflowResult value={item} schema={record(schema?.items)} />
          </li>
        ))}
      </ul>
    );
  const fields = record(value);
  if (fields)
    return (
      <dl className="grid min-w-0 gap-3">
        {Object.entries(fields).map(([key, item]) => {
          const field = record(record(schema?.properties)?.[key]);
          return (
            <div key={key} className="grid min-w-0 gap-1">
              <dt className="text-content-secondary type-caption font-medium">
                {typeof field?.title === "string" ? field.title : key}
              </dt>
              <dd className="type-body min-w-0 break-words">
                <WorkflowResult value={item} schema={field} />
              </dd>
            </div>
          );
        })}
      </dl>
    );
  return (
    <p className="type-body">
      {typeof value === "boolean"
        ? t(value ? "valueTrue" : "valueFalse")
        : String(value)}
    </p>
  );
}

type WorkerRequestView = {
  id: string;
  chatId?: string;
  kind: "approval" | "elicitation";
  active: boolean;
  onSettled: () => void;
};

export function InstanceDetails({
  id,
  runId,
  instanceId,
  human,
  request,
}: {
  id: string;
  runId: string;
  instanceId: string;
  human?: HumanRequest;
  request?: WorkerRequestView;
}) {
  const { t, i18n } = useTranslation("workflow");
  const active = useRouteSceneActive() && (request?.active ?? true);
  const instanceInput = { path: { id, runId, instanceId } };
  const instanceKey = getWorkflowInstanceQueryKey(instanceInput);
  const instanceQuery = useQuery({
    ...getWorkflowInstanceOptions(withLocalErrorSurface(instanceInput)),
    queryKey: instanceKey,
    enabled: Boolean(instanceId) && (!request || (active && !request.chatId)),
  });
  const workerInput = { path: { chatId: request?.chatId ?? "" } };
  const workerKey = getChatQueryKey(workerInput);
  const workerQuery = useQuery({
    ...getChatOptions(withLocalErrorSurface(workerInput)),
    queryKey: workerKey,
    enabled: active && Boolean(request?.chatId),
  });
  const query = request?.chatId ? workerQuery : instanceQuery;

  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id, parentId: runId },
    queryKeys: [instanceKey],
    active,
    enabled: Boolean(id && runId) && (!request || (active && !request.chatId)),
    eventTypes: ["workflow.run.changed", "workflow.worker.skip_requested"],
  });
  const workerChatId =
    request?.chatId ||
    instanceQuery.data?.chat?.chat?.id ||
    instanceQuery.data?.instance?.chatId ||
    "";
  useQueryEvents({
    topic: "chat",
    resource: { kind: "chat", id: workerChatId },
    queryKeys: [request?.chatId ? workerKey : instanceKey],
    active,
    enabled: Boolean(workerChatId) && (!request || active),
    eventTypes: ["execution.", "approval.", "elicitation."],
  });
  const settled = request
    ? () => {
        void query.refetch();
        request.onSettled();
      }
    : undefined;
  const resolveApproval = useMutation({
    ...withLocalErrorMutation(resolveApprovalMutation()),
    onSettled: settled,
  });
  const resolveElicitation = useMutation({
    ...withLocalErrorMutation(resolveElicitationMutation()),
    onSettled: settled,
  });
  const data = instanceQuery.data;
  const instance = data?.instance;
  const firstInput = instance?.input?.[0]?.data;
  const evaluatedOptions = record(
    instance?.input?.[instance.kind === "action" ? 1 : 0]?.data,
  );
  const businessInput =
    instance?.kind === "worker" || instance?.kind === "askUser"
      ? typeof firstInput === "string"
        ? firstInput
        : undefined
      : evaluatedOptions?.input;
  const resultEnvelope = record(instance?.output);
  const businessOutput =
    instance?.kind === "askUser"
      ? humanAnswerLabel(resultEnvelope?.value, human)
      : instance?.kind === "action" &&
          resultEnvelope &&
          Object.hasOwn(resultEnvelope, "data")
        ? resultEnvelope.data
        : instance?.output;
  const chat = request?.chatId ? workerQuery.data?.chat : data?.chat?.chat;
  const currentExecution = request?.chatId
    ? chat?.id === request.chatId && Boolean(chat.activeExecutionId)
    : Boolean(
        instance?.executionId &&
        chat?.activeExecutionId === instance.executionId,
      );
  const approvals = currentExecution ? (chat?.pendingApprovals ?? []) : [];
  const elicitations = currentExecution
    ? (chat?.pendingElicitations ?? [])
    : [];
  const approval = request
    ? approvals.find(
        (item) => request.kind === "approval" && item.approvalId === request.id,
      )
    : approvals[0];
  const elicitation = request
    ? elicitations.find(
        (item) =>
          request.kind === "elicitation" && item.elicitationId === request.id,
      )
    : elicitations[0];
  const error =
    resolveApproval.error ?? resolveElicitation.error ?? query.error;
  const refresh = () => void query.refetch();
  const controls =
    active && !instance?.completedAt && chat?.id ? (
      <>
        {approval ? (
          <div className="bg-surface-module rounded-lg">
            <ChatApprovalCard
              approval={approval}
              current={approvals.indexOf(approval) + 1}
              hotkeys={!request}
              total={approvals.length}
              submissionState={
                resolveApproval.isPending ? "submitting" : "idle"
              }
              onResolve={(approvalId, optionId) =>
                resolveApproval.mutate({
                  path: { chatId: chat.id ?? "", approvalId },
                  body: { optionId },
                })
              }
            />
          </div>
        ) : null}
        {elicitation ? (
          <div className="bg-surface-module overflow-hidden rounded-lg">
            <ChatElicitationCard
              elicitation={elicitation}
              submissionState={
                resolveElicitation.isPending ? "submitting" : "idle"
              }
              onResolve={(elicitationId, content, declined) =>
                resolveElicitation.mutate({
                  path: { chatId: chat.id ?? "", elicitationId },
                  body: { content, declined },
                })
              }
            />
          </div>
        ) : null}
      </>
    ) : null;
  const requestFeedback =
    query.isPending && active ? (
      request ? (
        <WorkflowRequestLoadingState />
      ) : (
        <WorkflowLoadingState />
      )
    ) : query.isError ? (
      <WorkflowQueryError
        error={query.error}
        retrying={query.isFetching}
        onRetry={refresh}
      />
    ) : error ? (
      <p role="alert" className="text-destructive type-body">
        {localApiErrorMessage(error, t("error"))}
      </p>
    ) : null;
  if (request) {
    return (
      <div className="grid gap-2">
        {chat?.wakerId && (approval || elicitation) ? (
          <WorkerReference
            showModel={false}
            resolve={{ wakerId: chat.wakerId }}
          />
        ) : null}
        {requestFeedback}
        {controls}
      </div>
    );
  }
  return (
    <section className="grid min-w-0 gap-4">
      {requestFeedback}
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
        />
      ) : null}
      {data?.effect?.status === "unknown" && !instance?.failure ? (
        <p role="alert" className="text-destructive type-body">
          {t("uncertainFailureHelp")}
        </p>
      ) : null}
      {controls}
      {instance ? (
        <>
          {businessInput !== undefined &&
          (instance.kind !== "askUser" || instance.completedAt) ? (
            <section className="grid gap-2">
              <h3 className="text-muted-foreground type-body font-medium">
                {t(
                  instance.kind === "worker" || instance.kind === "askUser"
                    ? "instruction"
                    : "stepInputs",
                )}
              </h3>
              {instance.kind === "worker" || instance.kind === "askUser" ? (
                <div className="bg-card max-h-80 overflow-y-auto rounded-lg p-3">
                  <WorkflowResult value={businessInput} />
                </div>
              ) : (
                <RunJsonValue value={businessInput} />
              )}
            </section>
          ) : null}
          {instance.completedAt ||
          instance.failure ||
          instance.output != null ? (
            <div className="grid gap-2">
              <h3 className="type-body font-medium">
                {instance.failure ? t("failure") : t("output")}
              </h3>
              {instance.failure ? (
                <div className="bg-card max-h-80 overflow-y-auto rounded-lg p-3">
                  <WorkflowFailure value={instance.failure.data} />
                </div>
              ) : businessOutput == null ? (
                <p className="text-muted-foreground type-body">
                  {t("emptyValue")}
                </p>
              ) : (
                <RunJsonValue value={businessOutput} />
              )}
            </div>
          ) : null}
        </>
      ) : null}
      {(data?.attempts?.length ?? 0) > 1 ? (
        <section className="grid gap-2">
          <h3 className="type-body font-medium">{t("attemptHistory")}</h3>
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
    </section>
  );
}
