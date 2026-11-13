import {
  Check,
  CornerDownLeft,
  ExternalLink,
  Loader2,
  MessageCircleQuestion,
} from "lucide-react";
import { useCallback, useMemo, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { cn } from "@/shared/lib/cn";
import type {
  AllyChatMessage,
  ChatPendingElicitation,
} from "../runtime/chat-projection";

export type ElicitationSubmissionState = "idle" | "submitting" | "confirming";

type SchemaField = {
  key: string;
  title: string;
  description?: string;
  options: Array<{ value: string; label: string; description?: string }>;
  multiSelect: boolean;
  minItems: number;
  customKey?: string;
  secret?: boolean;
};

type ParsedSchema = {
  fields: SchemaField[];
  required: string[];
  answerGroups: { keys: string[]; exclusive: boolean }[];
};

type Question = NonNullable<AllyChatMessage["question"]>;

type ChatElicitationCardProps = {
  elicitation: ChatPendingElicitation;
  resolution?: Question;
  submissionState?: ElicitationSubmissionState;
  waitingForTurn?: boolean;
  onResolve?: (
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => void;
};

function parseRequestedSchema(raw: unknown): ParsedSchema | null {
  if (!raw || typeof raw !== "object") return null;
  const schema = raw as Record<string, unknown>;
  const properties = schema.properties;
  if (!properties || typeof properties !== "object") return null;

  const customByQuestion = new Map<string, string>();
  for (const [key, value] of Object.entries(
    properties as Record<string, unknown>,
  )) {
    if (!key.endsWith("_custom")) continue;
    if (!value || typeof value !== "object") continue;
    const field = value as Record<string, unknown>;
    const meta = field._meta as Record<string, unknown> | undefined;
    const askMeta = meta?._askUserQuestionCustomAnswer as
      Record<string, unknown> | undefined;
    const questionId =
      typeof askMeta?.questionId === "string"
        ? askMeta.questionId
        : key.replace(/_custom$/, "");
    customByQuestion.set(questionId, key);
  }

  const fields: SchemaField[] = [];
  for (const [key, value] of Object.entries(
    properties as Record<string, unknown>,
  )) {
    if (key.endsWith("_custom") || key.endsWith("__other")) continue;
    if (!value || typeof value !== "object") continue;
    const field = value as Record<string, unknown>;
    const title =
      typeof field.title === "string" && field.title.trim() ? field.title : key;
    const description =
      typeof field.description === "string" ? field.description : undefined;
    const codexMeta = (field._meta as Record<string, unknown> | undefined)
      ?.codex as Record<string, unknown> | undefined;
    if (codexMeta?.isOtherAnswer) continue;
    const secret = Boolean(codexMeta?.isSecret);
    const options = extractOptions(field);
    const multiSelect =
      field.type === "array" ||
      (Array.isArray(field.type) && field.type.includes("array"));
    fields.push({
      key,
      title,
      description,
      options,
      multiSelect,
      minItems: typeof field.minItems === "number" ? field.minItems : 0,
      customKey:
        customByQuestion.get(key) ??
        findOtherFieldKey(key, properties as Record<string, unknown>),
      secret,
    });
  }
  if (!fields.length) return null;
  const required = requiredKeys(schema.required);
  // 已发出的单选卡使用 oneOf，多选卡使用 anyOf 并允许补充「其他」。
  const answerGroups = (
    Array.isArray(schema.allOf) ? schema.allOf : []
  ).flatMap((group: unknown) => {
    if (!group || typeof group !== "object" || Object.keys(group).length !== 1)
      return [];
    const exclusive = "oneOf" in group;
    const choices = exclusive
      ? group.oneOf
      : "anyOf" in group
        ? group.anyOf
        : undefined;
    if (!Array.isArray(choices)) return [];
    const keys = choices.flatMap((option: unknown) => {
      if (
        !option ||
        typeof option !== "object" ||
        !("required" in option) ||
        Object.keys(option).length !== 1
      )
        return [];
      const keys = requiredKeys(option.required);
      return keys.length === 1 ? keys : [];
    });
    return keys.length > 1 && keys.length === choices.length
      ? [{ keys, exclusive }]
      : [];
  });
  return { fields, required, answerGroups };
}

function requiredKeys(raw: unknown): string[] {
  return Array.isArray(raw)
    ? raw.filter((key): key is string => typeof key === "string")
    : [];
}

function findOtherFieldKey(
  questionId: string,
  properties: Record<string, unknown>,
): string | undefined {
  const candidate = `${questionId}__other`;
  if (properties[candidate]) return candidate;
  for (const [key, value] of Object.entries(properties)) {
    if (!value || typeof value !== "object") continue;
    const field = value as Record<string, unknown>;
    const codexMeta = (field._meta as Record<string, unknown> | undefined)
      ?.codex as Record<string, unknown> | undefined;
    if (codexMeta?.isOtherAnswer && codexMeta.questionId === questionId) {
      return key;
    }
  }
  return undefined;
}

function extractOptions(field: Record<string, unknown>) {
  const enumValues = Array.isArray(field.enum)
    ? field.enum
    : (field.items as Record<string, unknown> | undefined)?.enum;
  if (Array.isArray(enumValues)) {
    return enumValues.flatMap((value) =>
      typeof value === "string"
        ? [{ value, label: value, description: undefined }]
        : [],
    );
  }
  const source =
    (Array.isArray(field.oneOf) && field.oneOf) ||
    ((field.items as Record<string, unknown> | undefined)
      ?.anyOf as unknown[]) ||
    [];
  return source.flatMap((entry) => {
    if (!entry || typeof entry !== "object") return [];
    const option = entry as Record<string, unknown>;
    const value =
      typeof option.const === "string"
        ? option.const
        : typeof option.title === "string"
          ? option.title
          : "";
    if (!value) return [];
    return [
      {
        value,
        label:
          typeof option.title === "string" && option.title.trim()
            ? option.title
            : value,
        description:
          typeof option.description === "string"
            ? option.description
            : undefined,
      },
    ];
  });
}

export function ChatElicitationCard({
  elicitation,
  resolution,
  submissionState = "idle",
  waitingForTurn = false,
  onResolve,
}: ChatElicitationCardProps) {
  const { t } = useTranslation("chat");
  const elicitationId = elicitation.elicitationId ?? "";
  const parsedSchema = useMemo(
    () => parseRequestedSchema(elicitation.requestedSchema),
    [elicitation.requestedSchema],
  );
  const [values, setValues] = useState<Record<string, string | string[]>>({});
  const [customFields, setCustomFields] = useState<Record<string, boolean>>({});

  const updateValue = useCallback((key: string, value: string | string[]) => {
    setValues((current) => ({ ...current, [key]: value }));
  }, []);

  const content = useMemo(() => {
    const content: Record<string, unknown> = {};
    for (const field of parsedSchema?.fields ?? []) {
      const value = values[field.key];
      if (field.multiSelect) {
        const selected = Array.isArray(value) ? value : value ? [value] : [];
        if (selected.length || !field.minItems) content[field.key] = selected;
      } else if (typeof value === "string" && value.trim()) {
        content[field.key] = field.options.length ? value : value.trim();
      }
      if (field.customKey) {
        const customValue = values[field.customKey];
        if (typeof customValue === "string" && customValue.trim()) {
          content[field.customKey] = customValue.trim();
        }
      }
    }
    return content;
  }, [parsedSchema?.fields, values]);
  const canSubmit =
    !parsedSchema ||
    (parsedSchema.required.every((key) => Object.hasOwn(content, key)) &&
      parsedSchema.answerGroups.every(({ keys, exclusive }) => {
        const count = keys.filter((key) => Object.hasOwn(content, key)).length;
        return exclusive ? count === 1 : count > 0;
      }) &&
      parsedSchema.fields.every((field) => {
        const selection = content[field.key];
        if (Array.isArray(selection) && selection.length < field.minItems)
          return false;
        return (
          !field.multiSelect ||
          !field.minItems ||
          !field.customKey ||
          !customFields[field.key] ||
          Object.hasOwn(content, field.customKey)
        );
      }));
  const submit = useCallback(() => {
    if (!elicitationId || !canSubmit) return;
    onResolve?.(elicitationId, content, false);
  }, [canSubmit, content, elicitationId, onResolve]);

  const decline = useCallback(() => {
    if (!elicitationId) return;
    onResolve?.(elicitationId, {}, true);
  }, [elicitationId, onResolve]);

  const isSubmitting = submissionState !== "idle";
  const disabled = isSubmitting || waitingForTurn;
  const queueStatus = waitingForTurn ? (
    <p className="text-muted-foreground type-ui">
      {t("elicitation.waitingForTurn")}
    </p>
  ) : null;
  const cardProps = {
    "data-chat-elicitation-card": true,
    "data-message-id": elicitationId,
    "aria-busy": isSubmitting || undefined,
    className:
      "bg-surface-raised w-full max-w-chat-question space-y-3 rounded-2xl px-4 py-3",
  };
  const submitOnEnter = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      event.key !== "Enter" ||
      event.nativeEvent.isComposing ||
      event.keyCode === 229
    )
      return;
    event.preventDefault();
    event.stopPropagation();
    if (!disabled) submit();
  };

  const message = elicitation.message?.trim();
  const showMessage =
    message &&
    !parsedSchema?.fields.some(
      (field) =>
        field.title.trim() === message || field.description?.trim() === message,
    );

  if (resolution?.result) {
    const fields = parsedSchema?.fields ?? [];
    const resultLabel = (
      <span className="text-muted-foreground type-ui ms-auto shrink-0 whitespace-nowrap">
        {t(`elicitation.result.${resolution.result}`)}
      </span>
    );
    return (
      <article {...cardProps}>
        {showMessage || fields.length === 0 ? (
          <div className="flex items-start gap-3">
            {showMessage ? (
              <p className="type-title min-w-0 font-medium break-words whitespace-pre-wrap">
                {message}
              </p>
            ) : null}
            {resultLabel}
          </div>
        ) : null}
        {fields.map((field, fieldIndex) => {
          const question = fieldQuestion(field);
          const value = resolution.answers?.[field.key];
          const custom = field.customKey
            ? resolution.answers?.[field.customKey]
            : undefined;
          const answers = field.secret
            ? []
            : (Array.isArray(value) ? value : [value]).flatMap((item) => {
                if (typeof item !== "string" || !item.trim()) return [];
                const index = field.options.findIndex(
                  (option) => option.value === item,
                );
                return [
                  {
                    label: field.options[index]?.label ?? item,
                    index: index < 0 ? field.options.length : index,
                  },
                ];
              });
          if (!field.secret && typeof custom === "string" && custom.trim()) {
            answers.push({ label: custom, index: field.options.length });
          }
          return (
            <div key={field.key} className="space-y-1">
              <div className="flex items-start gap-3">
                <p className="type-label min-w-0 font-medium break-words whitespace-pre-wrap">
                  {question}
                </p>
                {!showMessage && fieldIndex === 0 ? resultLabel : null}
              </div>
              {field.options.length === 0 && field.description ? (
                <p className="text-muted-foreground type-caption break-words whitespace-pre-wrap">
                  {field.description}
                </p>
              ) : null}
              {answers.length > 0 ? (
                <ul className="type-body space-y-1">
                  {answers.map((answer) => (
                    <li
                      key={`${answer.index}:${answer.label}`}
                      className="flex items-start gap-2 break-words whitespace-pre-wrap"
                    >
                      <ElicitationOptionLetter index={answer.index} selected />
                      <span className="min-w-0">{answer.label}</span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          );
        })}
      </article>
    );
  }

  if (elicitation.mode === "url" && elicitation.url) {
    return (
      <article {...cardProps}>
        <div className="flex items-start gap-2">
          <MessageCircleQuestion className="text-muted-foreground mt-0.5 size-4 shrink-0" />
          <div className="min-w-0 space-y-1">
            <p className="type-label">{elicitation.message}</p>
            <a
              href={elicitation.url}
              target="_blank"
              rel="noreferrer"
              className="text-link type-caption inline-flex items-center gap-1 break-all hover:underline"
            >
              {elicitation.url}
              <ExternalLink className="size-3.5 shrink-0" />
            </a>
          </div>
        </div>
        {queueStatus}
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={decline}
          >
            {t("elicitation.decline")}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={disabled}
            onClick={() => onResolve?.(elicitationId, {}, false)}
          >
            {isSubmitting ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              t("elicitation.continue")
            )}
          </Button>
        </div>
      </article>
    );
  }

  return (
    <article {...cardProps}>
      {showMessage ? (
        <p className="type-title font-medium break-words whitespace-pre-wrap">
          {message}
        </p>
      ) : null}
      {parsedSchema ? (
        <div className="space-y-5 p-1">
          {parsedSchema.fields.map((field) => {
            const customKey = field.customKey;
            const customOpen = Boolean(customFields[field.key]);
            const question = fieldQuestion(field);
            const currentValue = values[field.key];
            return (
              <fieldset
                key={field.key}
                className="min-w-0 space-y-2"
                disabled={disabled}
              >
                <legend className="type-title mb-2 font-medium break-words whitespace-pre-wrap">
                  {question}
                </legend>
                {field.options.length === 0 && field.description ? (
                  <p className="text-muted-foreground type-caption">
                    {field.description}
                  </p>
                ) : null}
                {field.multiSelect ? (
                  <p className="text-muted-foreground type-caption">
                    {t("elicitation.multipleChoiceHint")}
                  </p>
                ) : null}
                {field.options.length > 0 ? (
                  <div className="flex flex-col gap-1">
                    {field.options.map((option, index) => {
                      const selected = field.multiSelect
                        ? Array.isArray(currentValue) &&
                          currentValue.includes(option.value)
                        : currentValue === option.value;
                      return (
                        <ElicitationChoice
                          key={option.value}
                          index={index}
                          label={option.label}
                          description={option.description}
                          selected={selected}
                          disabled={disabled}
                          onClick={() => {
                            if (field.multiSelect) {
                              const current = Array.isArray(currentValue)
                                ? currentValue
                                : [];
                              updateValue(
                                field.key,
                                selected
                                  ? current.filter(
                                      (value) => value !== option.value,
                                    )
                                  : [...current, option.value],
                              );
                              return;
                            }
                            updateValue(field.key, option.value);
                            if (customKey) updateValue(customKey, "");
                            setCustomFields((current) => ({
                              ...current,
                              [field.key]: false,
                            }));
                          }}
                        />
                      );
                    })}
                    {customKey ? (
                      <ElicitationChoice
                        index={field.options.length}
                        label={t("elicitation.other")}
                        selected={customOpen}
                        disabled={disabled}
                        onClick={() => {
                          setCustomFields((current) => ({
                            ...current,
                            [field.key]: !customOpen,
                          }));
                          if (customOpen) updateValue(customKey, "");
                          if (!field.multiSelect) updateValue(field.key, "");
                        }}
                      />
                    ) : null}
                  </div>
                ) : (
                  <Input
                    aria-label={question}
                    placeholder={t("elicitation.answerPlaceholder")}
                    onKeyDown={submitOnEnter}
                    type={field.secret ? "password" : "text"}
                    value={typeof currentValue === "string" ? currentValue : ""}
                    disabled={disabled}
                    onChange={(event) =>
                      updateValue(field.key, event.target.value)
                    }
                  />
                )}
                {customKey && customOpen ? (
                  <div className="pl-8">
                    <Input
                      aria-label={t("elicitation.other")}
                      type={field.secret ? "password" : "text"}
                      placeholder={t("elicitation.otherPlaceholder")}
                      onKeyDown={submitOnEnter}
                      value={
                        typeof values[customKey] === "string"
                          ? values[customKey]
                          : ""
                      }
                      disabled={disabled}
                      onChange={(event) =>
                        updateValue(customKey, event.target.value)
                      }
                    />
                  </div>
                ) : null}
              </fieldset>
            );
          })}
        </div>
      ) : null}
      {queueStatus}
      <div className="flex flex-wrap justify-end gap-2 pt-1">
        {elicitation.allowSkip !== false ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={decline}
          >
            {t("elicitation.skip")}
          </Button>
        ) : null}
        <Button
          type="button"
          size="sm"
          disabled={disabled || !canSubmit}
          onClick={submit}
        >
          {isSubmitting ? <Loader2 className="animate-spin" /> : null}
          {t("elicitation.next")}
          {!isSubmitting ? <CornerDownLeft aria-hidden="true" /> : null}
        </Button>
      </div>
    </article>
  );
}

function fieldQuestion(field: SchemaField) {
  return field.options.length > 0
    ? field.description || field.title
    : field.title;
}

function ElicitationChoice({
  index,
  label,
  description,
  selected,
  disabled,
  onClick,
}: {
  index: number;
  label: string;
  description?: string;
  selected: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="choice"
      size="option"
      aria-pressed={selected}
      disabled={disabled}
      onClick={onClick}
    >
      <ElicitationOptionLetter index={index} selected={selected} />
      <span className="min-w-0 flex-1 text-left">
        <span className="block break-words whitespace-pre-wrap">{label}</span>
        {description ? (
          <span className="text-muted-foreground type-caption mt-0.5 block font-normal break-words whitespace-pre-wrap">
            {description}
          </span>
        ) : null}
      </span>
      {selected ? (
        <Check
          aria-hidden="true"
          className="text-accent-content mt-0.5 shrink-0"
        />
      ) : null}
    </Button>
  );
}

function ElicitationOptionLetter({
  index,
  selected,
}: {
  index: number;
  selected: boolean;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "type-ui size-icon-lg flex shrink-0 items-center justify-center rounded-xs font-medium",
        selected
          ? "bg-accent text-accent-foreground"
          : "bg-accent-surface-strong text-accent-content",
      )}
    >
      {index < 26 ? String.fromCharCode(65 + index) : index + 1}
    </span>
  );
}
