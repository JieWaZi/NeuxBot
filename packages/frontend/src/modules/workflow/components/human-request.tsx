import { humanAnswerLabel } from "../model/human-answer";
import {
  useWorkflowDecisionDraft,
  workflowDecisionDraftKey,
} from "../model/human-answer-draft";
import { WorkflowResult } from "./instance-details";
import { Input } from "@/shared/ui/input";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useId, useState, type ReactNode } from "react";
import {
  QuestionCard,
  QuestionChoice,
  QuestionActions,
  QuestionResultHeader,
} from "@/shared/ui/question-card";
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
import { answerWorkflowHumanMutation } from "../api/workflow-queries";
import {
  isWorkflowDecisionConflict,
  refreshWorkflowDecisionQueries,
} from "../api/workflow-decision-queries";
import { record, jsonText, type HumanRequest } from "../model/types";
import { ReadOnlyMarkdown } from "@/modules/chat";
import {
  SchemaForm,
  schemaDefaults,
  useSchemaFormValidity,
} from "./schema-form";
import type { SchemaFormDraft } from "@/shared/json-schema/schema-form";

const emptySchemaDraft: SchemaFormDraft = {};

type HumanRequestProps = {
  id: string;
  runId: string;
  chatId?: string;
  human: HumanRequest;
  onAnswered?: () => void;
  navigationAction?: ReactNode;
};

export function WorkflowChatQuestion(
  props: HumanRequestProps & { waitingForTurn?: boolean },
) {
  return <HumanRequestCard {...props} inChat />;
}

export function HumanRequestCard(
  props: HumanRequestProps & { inChat?: boolean; waitingForTurn?: boolean },
) {
  const draftKey = workflowDecisionDraftKey(
    "human",
    props.id,
    props.runId,
    props.human.id,
    props.human.snapshotDigest,
  );
  return <HumanRequestContent key={draftKey} draftKey={draftKey} {...props} />;
}

function HumanRequestContent({
  id,
  runId,
  chatId,
  human,
  draftKey,
  onAnswered,
  navigationAction,
  inChat = false,
  waitingForTurn = false,
}: HumanRequestProps & {
  draftKey?: string;
  inChat?: boolean;
  waitingForTurn?: boolean;
}) {
  const controlId = useId();
  const { t: chatText } = useTranslation("chat");
  const { t, i18n } = useTranslation("workflow");
  const client = useQueryClient();
  const options = human.options ?? {};
  const rawChoices = options.choices ?? options.options;
  const choices = Array.isArray(rawChoices)
    ? rawChoices.flatMap((item) => {
        const choice = record(item);
        return choice &&
          typeof choice.value === "string" &&
          typeof choice.label === "string"
          ? [{ value: choice.value, label: choice.label }]
          : [];
      })
    : [];
  const { draft, update, start, finish } = useWorkflowDecisionDraft(
    draftKey,
    human.status,
  );
  const customChoice = draft.customChoice ?? "";
  const setCustomChoice = (value: string) => update({ customChoice: value });
  const [search, setSearch] = useState("");
  const [choicePage, setChoicePage] = useState(0);
  const filtered = choices.filter((choice) =>
    `${choice.label} ${choice.value}`
      .toLocaleLowerCase()
      .includes(search.toLocaleLowerCase()),
  );
  const pageChoices = filtered.slice(choicePage * 20, (choicePage + 1) * 20);
  const multi =
    options.kind === "multiselect" ||
    (!options.kind && options.multiSelect === true);
  const confirm = options.kind === "confirm";
  const initial = options.defaultValue;
  const schema =
    options.kind === "form" || !options.kind
      ? record(options.schema)
      : undefined;
  const validity = useSchemaFormValidity();
  const [defaultAnswer] = useState<unknown>(() =>
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
  const answer = "answer" in draft ? draft.answer : defaultAnswer;
  const setAnswer = (value: unknown) => update({ answer: value });
  const updateSchemaDraft = useCallback(
    (change: (current: SchemaFormDraft) => SchemaFormDraft) => {
      update((current) => {
        const before = current.schema ?? emptySchemaDraft;
        const schema = change(before);
        return schema === before ? current : { ...current, schema };
      });
    },
    [update],
  );
  const submit = useMutation(
    withLocalErrorMutation(answerWorkflowHumanMutation()),
  );
  const snapshotReady = Boolean(human.id && human.snapshotDigest);
  const submission = draft.submission;
  const submitting = submission?.status === "pending";
  const disabled =
    waitingForTurn || Boolean(submission && submission.status !== "failed");
  const send = async (skip: boolean) => {
    const snapshotDigest = human.snapshotDigest;
    if (
      !snapshotReady ||
      typeof snapshotDigest !== "string" ||
      disabled ||
      human.status !== "waiting" ||
      (!skip && !canSubmit)
    )
      return;
    const signature = jsonText({
      answer,
      skip,
      snapshotDigest: human.snapshotDigest,
    });
    const requestId = start(signature);
    if (!requestId) return;
    try {
      await submit.mutateAsync({
        path: { id, runId, humanId: human.id ?? "" },
        body: {
          requestId,
          value: answer,
          skip,
          snapshotDigest,
        },
      });
      finish(requestId, "accepted");
    } catch (error) {
      if (isWorkflowDecisionConflict(error)) {
        finish(requestId, "conflict", error);
        void refreshWorkflowDecisionQueries(client, id, runId, chatId);
      } else {
        finish(requestId, "failed", error);
      }
      return;
    }
    onAnswered?.();
  };
  const waiting = human.status === "waiting";
  const canSubmit =
    snapshotReady &&
    validity.valid &&
    !Object.values(draft.schema ?? emptySchemaDraft).some(
      (field) => field.json?.invalid,
    ) &&
    !(
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
    );
  const simpleChoices = choices.length <= 20 && options.allowCustom !== true;
  const prompt = <ReadOnlyMarkdown source={human.prompt ?? ""} />;
  const fields = (
    <div className="grid gap-3">
      {!inChat && (
        <h3 className="type-label font-medium">{t("waitingHuman")}</h3>
      )}
      {inChat && !waiting ? (
        <QuestionResultHeader
          status={human.status ? t(`status.${human.status}`) : undefined}
        >
          <div className="min-w-0 font-medium">{prompt}</div>
        </QuestionResultHeader>
      ) : (
        prompt
      )}
      {!inChat && (
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
      )}
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
          <label htmlFor={`search-${controlId}`} className="type-body">
            {t("choiceSearch")}
          </label>
          <Input
            id={`search-${controlId}`}
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
              {choicePage + 1} / {Math.max(1, Math.ceil(filtered.length / 20))}
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
        confirm || (inChat && choices.length > 0 && simpleChoices) ? (
          <div className="flex flex-col gap-1">
            {(confirm
              ? [
                  { value: true, label: t("true") },
                  { value: false, label: t("false") },
                ]
              : choices
            ).map((choice, index) => (
              <QuestionChoice
                key={String(choice.value)}
                index={index}
                label={choice.label}
                selected={
                  Array.isArray(answer)
                    ? answer.includes(choice.value)
                    : answer === choice.value
                }
                disabled={disabled}
                onClick={() => {
                  if (!multi) {
                    setAnswer(choice.value);
                    return;
                  }
                  const values = Array.isArray(answer) ? answer : [];
                  setAnswer(
                    values.includes(choice.value)
                      ? values.filter((item) => item !== choice.value)
                      : [...values, choice.value],
                  );
                }}
              />
            ))}
          </div>
        ) : schema ? (
          <SchemaForm
            compact
            schema={schema}
            value={answer}
            onChange={setAnswer}
            onValidityChange={validity.onValidityChange}
            editingDraft={{
              fields: draft.schema ?? emptySchemaDraft,
              update: updateSchemaDraft,
            }}
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
                  <label htmlFor={`custom-${controlId}`} className="type-body">
                    {t("answerCustom")}
                  </label>
                  <Input
                    id={`custom-${controlId}`}
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
                        values.includes(custom) ? values : [...values, custom],
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
                    htmlFor={`answer-${controlId}`}
                    className="type-caption text-muted-foreground"
                  >
                    {t("answerCustom")}
                  </label>
                  <Textarea
                    id={`answer-${controlId}`}
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
            <label htmlFor={`answer-${controlId}`} className="type-body">
              {t("waitingHuman")}
            </label>
            <Textarea
              id={`answer-${controlId}`}
              placeholder={t("answerPlaceholder")}
              value={typeof answer === "string" ? answer : ""}
              onChange={(event) => setAnswer(event.target.value)}
            />
          </div>
        )
      ) : (
        <WorkflowResult value={humanAnswerLabel(human.value, human)} />
      )}
      {waiting && inChat ? (
        <QuestionActions
          canSubmit={canSubmit}
          disabled={disabled || !snapshotReady}
          submitting={submitting}
          onSubmit={() => void send(false)}
          onSkip={
            options.allowSkip === true ? () => void send(true) : undefined
          }
        />
      ) : waiting ? (
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            disabled={!canSubmit || disabled}
            onClick={() => void send(false)}
          >
            {t("answer")}
          </Button>
          {options.allowSkip === true ? (
            <Button
              size="sm"
              variant="secondary"
              disabled={disabled || !snapshotReady}
              onClick={() => void send(true)}
            >
              {t("skip")}
            </Button>
          ) : null}
        </div>
      ) : null}
      {waiting &&
      (submission?.status === "failed" || submission?.status === "conflict") ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(submission.error, t("answerError"))}
        </p>
      ) : null}
    </div>
  );
  return inChat ? (
    <QuestionCard id={human.id ?? ""} busy={submitting}>
      {waitingForTurn && waiting && (
        <p role="status" className="text-muted-foreground type-caption">
          {chatText("elicitation.waitingForTurn")}
        </p>
      )}
      <fieldset className="min-w-0" disabled={disabled}>
        {fields}
      </fieldset>
      {navigationAction}
    </QuestionCard>
  ) : (
    <Card variant="section">
      <fieldset className="min-w-0" disabled={disabled}>
        {fields}
      </fieldset>
    </Card>
  );
}
