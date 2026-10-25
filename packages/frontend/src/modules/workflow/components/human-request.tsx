import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Card } from "@/shared/ui/card";
import { Textarea } from "@/shared/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  answerWorkflowHumanMutation,
  getWorkflowRunQueryKey,
} from "../api/workflow-queries";
import { record, jsonText, type HumanRequest } from "../model/types";
import { ReadOnlyMarkdown } from "@/modules/chat";
import {
  SchemaForm,
  schemaDefaults,
  useSchemaFormValidity,
} from "./schema-form";

export function HumanRequestCard({
  id,
  runId,
  human,
}: {
  id: string;
  runId: string;
  human: HumanRequest;
}) {
  const { t, i18n } = useTranslation("workflow");
  const client = useQueryClient();
  const options = human.options ?? {};
  const choices = Array.isArray(options.options)
    ? options.options.flatMap((item) => {
        const choice = record(item);
        return choice &&
          typeof choice.value === "string" &&
          typeof choice.label === "string"
          ? [{ value: choice.value, label: choice.label }]
          : [];
      })
    : [];
  const multi = options.multiSelect === true;
  const initial = options.defaultValue;
  const schema = record(options.schema);
  const validity = useSchemaFormValidity();
  const [answer, setAnswer] = useState<unknown>(
    schema
      ? (initial ?? schemaDefaults(schema) ?? {})
      : multi
        ? Array.isArray(initial)
          ? initial.filter((item) => typeof item === "string")
          : []
        : typeof initial === "string"
          ? initial
          : "",
  );
  const [request, setRequest] = useState<{ signature: string; id: string }>();
  const submit = useMutation(
    withLocalErrorMutation(answerWorkflowHumanMutation()),
  );
  const send = async (skip: boolean) => {
    const signature = jsonText({ answer, skip });
    const requestId =
      request?.signature === signature ? request.id : crypto.randomUUID();
    setRequest({ signature, id: requestId });
    try {
      await submit.mutateAsync({
        path: { id, runId, humanId: human.id ?? "" },
        body: { requestId, value: answer, skip },
      });
      await client.invalidateQueries({
        queryKey: getWorkflowRunQueryKey({ path: { id, runId } }),
      });
    } catch {
      /* Mutation owns local error and preserves the draft. */
    }
  };
  const waiting = human.status === "waiting";
  return (
    <Card
      variant="section"
      {...notificationPresentationProps({
        kind: "request",
        noticeId: `human:${human.id}`,
      })}
    >
      <div className="grid gap-3">
        <h3 className="type-label font-medium">{t("waitingHuman")}</h3>
        <ReadOnlyMarkdown source={human.prompt ?? ""} />
        <p className="text-muted-foreground type-caption">
          {waiting
            ? t("answerHelp")
            : human.source === "user"
              ? t("userSource")
              : human.source === "skip"
                ? t("skipSource")
                : human.source === "timeout"
                  ? t("timeoutSource")
                  : human.status
                    ? t(`status.${human.status}`)
                    : ""}
        </p>
        {human.deadlineAt ? (
          <p className="text-muted-foreground type-caption">
            {t("deadline")} ·{" "}
            {new Intl.DateTimeFormat(i18n.language, {
              dateStyle: "short",
              timeStyle: "medium",
            }).format(human.deadlineAt)}
          </p>
        ) : null}
        {waiting ? (
          schema ? (
            <SchemaForm
              compact
              schema={schema}
              value={answer}
              onChange={setAnswer}
              onValidityChange={validity.onValidityChange}
            />
          ) : choices.length ? (
            multi ? (
              <div className="grid gap-2">
                {choices.map((choice) => (
                  <label
                    key={choice.value}
                    className="type-body flex items-center gap-2"
                  >
                    <Checkbox
                      checked={
                        Array.isArray(answer) && answer.includes(choice.value)
                      }
                      onCheckedChange={(checked) => {
                        const values = Array.isArray(answer) ? answer : [];
                        setAnswer(
                          checked === true
                            ? [...values, choice.value]
                            : values.filter((value) => value !== choice.value),
                        );
                      }}
                    />
                    {choice.label}
                  </label>
                ))}
              </div>
            ) : (
              <div className="grid gap-3">
                <Select
                  value={
                    typeof answer === "string" &&
                    choices.some((choice) => choice.value === answer)
                      ? answer
                      : ""
                  }
                  onValueChange={setAnswer}
                >
                  <SelectTrigger aria-label={human.prompt}>
                    <SelectValue placeholder={t("answerPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {choices.map((choice) => (
                      <SelectItem key={choice.value} value={choice.value}>
                        {choice.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <label
                  htmlFor={`answer-${human.id}`}
                  className="type-caption text-muted-foreground"
                >
                  {t("answerCustom")}
                </label>
                <Textarea
                  id={`answer-${human.id}`}
                  placeholder={t("answerPlaceholder")}
                  value={
                    typeof answer === "string" &&
                    !choices.some((choice) => choice.value === answer)
                      ? answer
                      : ""
                  }
                  onChange={(event) => setAnswer(event.target.value)}
                />
              </div>
            )
          ) : (
            <div className="grid gap-2">
              <label htmlFor={`answer-${human.id}`} className="type-body">
                {t("waitingHuman")}
              </label>
              <Textarea
                id={`answer-${human.id}`}
                placeholder={t("answerPlaceholder")}
                value={typeof answer === "string" ? answer : ""}
                onChange={(event) => setAnswer(event.target.value)}
              />
            </div>
          )
        ) : (
          <pre className="type-body whitespace-pre-wrap">
            {jsonText(human.value)}
          </pre>
        )}
        {waiting ? (
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={
                submit.isPending ||
                !validity.valid ||
                (!schema && !multi && answer === "")
              }
              onClick={() => void send(false)}
            >
              {t("answer")}
            </Button>
            {options.allowSkip === true ? (
              <Button
                size="sm"
                variant="secondary"
                disabled={submit.isPending}
                onClick={() => void send(true)}
              >
                {t("skip")}
              </Button>
            ) : null}
          </div>
        ) : null}
        {submit.error ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(submit.error, t("answerError"))}
          </p>
        ) : null}
      </div>
    </Card>
  );
}
