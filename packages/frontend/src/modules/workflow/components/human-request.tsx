import { humanAnswerLabel } from "../model/human-answer";
import { WorkflowResult } from "./instance-details";
import { Input } from "@/shared/ui/input";
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
  onAnswered,
}: {
  id: string;
  runId: string;
  human: HumanRequest;
  onAnswered?: () => void;
}) {
  const { t, i18n } = useTranslation("workflow");
  const client = useQueryClient();
  const options = human.options ?? {};
  const choices = Array.isArray(options.choices)
    ? options.choices.flatMap((item) => {
        const choice = record(item);
        return choice &&
          typeof choice.value === "string" &&
          typeof choice.label === "string"
          ? [{ value: choice.value, label: choice.label }]
          : [];
      })
    : [];
  const [customChoice, setCustomChoice] = useState("");
  const [search, setSearch] = useState("");
  const [choicePage, setChoicePage] = useState(0);
  const filtered = choices.filter((choice) =>
    `${choice.label} ${choice.value}`
      .toLocaleLowerCase()
      .includes(search.toLocaleLowerCase()),
  );
  const pageChoices = filtered.slice(choicePage * 20, (choicePage + 1) * 20);
  const multi = options.kind === "multiselect";
  const confirm = options.kind === "confirm";
  const initial = options.defaultValue;
  const schema = options.kind === "form" ? record(options.schema) : undefined;
  const validity = useSchemaFormValidity();
  const [answer, setAnswer] = useState<unknown>(
    schema
      ? (initial ?? schemaDefaults(schema) ?? {})
      : multi
        ? Array.isArray(initial)
          ? initial.filter((item) => typeof item === "string")
          : []
        : confirm
          ? typeof initial === "boolean"
            ? initial
            : undefined
          : typeof initial === "string"
            ? initial
            : "",
  );
  const [request, setRequest] = useState<{ signature: string; id: string }>();
  const submit = useMutation(
    withLocalErrorMutation(answerWorkflowHumanMutation()),
  );
  const snapshotReady =
    typeof human.questionVersion === "number" &&
    typeof human.snapshotDigest === "string";
  const send = async (skip: boolean) => {
    const questionVersion = human.questionVersion;
    const snapshotDigest = human.snapshotDigest;
    if (
      typeof questionVersion !== "number" ||
      typeof snapshotDigest !== "string"
    )
      return;
    const signature = jsonText({
      answer,
      skip,
      questionVersion: human.questionVersion,
      snapshotDigest: human.snapshotDigest,
    });
    const requestId =
      request?.signature === signature ? request.id : crypto.randomUUID();
    setRequest({ signature, id: requestId });
    try {
      await submit.mutateAsync({
        path: { id, runId, humanId: human.id ?? "" },
        body: {
          requestId,
          value: answer,
          skip,
          questionVersion,
          snapshotDigest,
        },
      });
      onAnswered?.();
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
              : human.source === "user_skip"
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
        {waiting && choices.length > 20 ? (
          <div className="grid gap-2">
            <label htmlFor={`search-${human.id}`} className="type-body">
              {t("choiceSearch")}
            </label>
            <Input
              id={`search-${human.id}`}
              placeholder={t("choiceSearch")}
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setChoicePage(0);
              }}
            />
            <p className="text-muted-foreground type-caption">
              {t("choiceSearchHelp")}
            </p>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="ghost"
                disabled={choicePage === 0}
                onClick={() => setChoicePage(choicePage - 1)}
              >
                {t("resultPage.previous")}
              </Button>
              <span className="type-caption">
                {choicePage + 1} /{" "}
                {Math.max(1, Math.ceil(filtered.length / 20))}
              </span>
              <Button
                size="sm"
                variant="ghost"
                disabled={(choicePage + 1) * 20 >= filtered.length}
                onClick={() => setChoicePage(choicePage + 1)}
              >
                {t("resultPage.next")}
              </Button>
            </div>
          </div>
        ) : null}
        {waiting ? (
          confirm ? (
            <label className="type-body flex items-center gap-2">
              <Checkbox
                checked={answer === true}
                onCheckedChange={(checked) => setAnswer(checked === true)}
              />
              {human.prompt}
            </label>
          ) : schema ? (
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
                {pageChoices.map((choice) => (
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
                {options.allowCustom === true ? (
                  <div className="grid gap-2">
                    <label htmlFor={`custom-${human.id}`} className="type-body">
                      {t("answerCustom")}
                    </label>
                    <Input
                      id={`custom-${human.id}`}
                      placeholder={t("answerPlaceholder")}
                      value={customChoice}
                      onChange={(event) => setCustomChoice(event.target.value)}
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={!customChoice.trim()}
                      onClick={() => {
                        const custom = customChoice.trim();
                        const values = Array.isArray(answer) ? answer : [];
                        setAnswer(
                          values.includes(custom)
                            ? values
                            : [...values, custom],
                        );
                        setCustomChoice("");
                      }}
                    >
                      {t("addAnswer")}
                    </Button>
                    {Array.isArray(answer)
                      ? answer
                          .filter(
                            (value) =>
                              typeof value === "string" &&
                              !choices.some((choice) => choice.value === value),
                          )
                          .map((value) => (
                            <label
                              key={value}
                              className="type-body flex items-center gap-2"
                            >
                              <Checkbox
                                checked
                                onCheckedChange={() =>
                                  setAnswer(
                                    answer.filter((entry) => entry !== value),
                                  )
                                }
                              />
                              {String(value)}
                            </label>
                          ))
                      : null}
                  </div>
                ) : null}
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
                    {pageChoices.map((choice) => (
                      <SelectItem key={choice.value} value={choice.value}>
                        {choice.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {options.allowCustom === true ? (
                  <>
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
                  </>
                ) : null}
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
          <WorkflowResult value={humanAnswerLabel(human.value, human)} />
        )}
        {waiting ? (
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={
                !snapshotReady ||
                submit.isPending ||
                !validity.valid ||
                (!schema && !multi && !confirm && answer === "") ||
                (confirm && typeof answer !== "boolean") ||
                (multi &&
                  (!Array.isArray(answer) ||
                    answer.length <
                      (typeof options.minSelections === "number"
                        ? options.minSelections
                        : 1) ||
                    answer.length >
                      (typeof options.maxSelections === "number"
                        ? options.maxSelections
                        : choices.length)))
              }
              onClick={() => void send(false)}
            >
              {t("answer")}
            </Button>
            {options.allowSkip === true ? (
              <Button
                size="sm"
                variant="secondary"
                disabled={submit.isPending || !snapshotReady}
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
