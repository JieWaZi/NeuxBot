import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import {
  Check,
  CornerDownLeft,
  ExternalLink,
  Loader2,
  MessageCircleQuestion,
} from "lucide-react";
import { useCallback, useMemo, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingElicitation as PendingElicitation } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";

export type ElicitationSubmissionState = "idle" | "submitting" | "confirming";

type SchemaField = {
  key: string;
  title: string;
  description?: string;
  options: Array<{ value: string; label: string; description?: string }>;
  multiSelect: boolean;
  customKey?: string;
  secret?: boolean;
};

type ParsedSchema = {
  fields: SchemaField[];
};

type ChatElicitationCardProps = {
  elicitation: Omit<PendingElicitation, "requestedSchema"> & {
    // 动态表单按未知 JSON 进入解析边界，不依赖生成器对 RawMessage 的数组推断。
    requestedSchema?: unknown;
  };
  submissionState: ElicitationSubmissionState;
  onResolve: (
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
    const field = value as Record<string, unknown>;
    const title =
      typeof field.title === "string" && field.title.trim() ? field.title : key;
    const description =
      typeof field.description === "string" ? field.description : undefined;
    const codexMeta = (field._meta as Record<string, unknown> | undefined)
      ?.codex as Record<string, unknown> | undefined;
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
      customKey:
        customByQuestion.get(key) ??
        findOtherFieldKey(key, properties as Record<string, unknown>),
      secret,
    });
  }
  return fields.length > 0 ? { fields } : null;
}

function findOtherFieldKey(
  questionId: string,
  properties: Record<string, unknown>,
): string | undefined {
  const candidate = `${questionId}__other`;
  if (properties[candidate]) return candidate;
  for (const [key, value] of Object.entries(properties)) {
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
  submissionState,
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

  const submit = useCallback(() => {
    if (!elicitationId) return;
    const content: Record<string, unknown> = {};
    for (const field of parsedSchema?.fields ?? []) {
      const value = values[field.key];
      if (field.multiSelect) {
        content[field.key] = Array.isArray(value)
          ? value
          : value
            ? [value]
            : [];
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
    onResolve(elicitationId, content, false);
  }, [elicitationId, onResolve, parsedSchema?.fields, values]);

  const decline = useCallback(() => {
    if (!elicitationId) return;
    onResolve(elicitationId, {}, true);
  }, [elicitationId, onResolve]);

  const isSubmitting = submissionState !== "idle";
  const submitOnEnter = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      event.key !== "Enter" ||
      event.nativeEvent.isComposing ||
      event.keyCode === 229
    )
      return;
    event.preventDefault();
    event.stopPropagation();
    if (!isSubmitting) submit();
  };

  if (elicitation.mode === "url" && elicitation.url) {
    return (
      <div
        data-chat-elicitation-card
        {...notificationPresentationProps({
          kind: "request",
          noticeId: `elicitation:${elicitation.elicitationId}`,
        })}
        className="border-border bg-surface-module space-y-3 border-b px-4 py-3"
      >
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
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isSubmitting}
            onClick={decline}
          >
            {t("elicitation.decline")}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={isSubmitting}
            onClick={() => onResolve(elicitationId, {}, false)}
          >
            {isSubmitting ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              t("elicitation.continue")
            )}
          </Button>
        </div>
      </div>
    );
  }

  const message = elicitation.message?.trim();
  const showMessage =
    message &&
    !parsedSchema?.fields.some(
      (field) =>
        field.title.trim() === message || field.description?.trim() === message,
    );

  return (
    <div
      data-chat-elicitation-card
      {...notificationPresentationProps({
        kind: "request",
        noticeId: `elicitation:${elicitation.elicitationId}`,
      })}
      className="border-border bg-surface-module space-y-3 border-b px-4 py-3"
    >
      <p className="text-muted-foreground type-ui">
        {t("elicitation.question")}
      </p>
      {showMessage ? <p className="type-label font-medium">{message}</p> : null}
      {parsedSchema ? (
        <div className="max-h-96 space-y-5 overflow-y-auto p-1">
          {parsedSchema.fields.map((field) => {
            const customKey = field.customKey;
            const customOpen = Boolean(customFields[field.key]);
            const question =
              field.options.length > 0
                ? field.description || field.title
                : field.title;
            const currentValue = values[field.key];
            return (
              <fieldset
                key={field.key}
                className="min-w-0 space-y-2"
                disabled={isSubmitting}
              >
                <legend className="type-label mb-2 font-medium break-words whitespace-pre-wrap">
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
                          disabled={isSubmitting}
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
                        disabled={isSubmitting}
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
                    disabled={isSubmitting}
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
                      disabled={isSubmitting}
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
      <div className="flex justify-end gap-2 pt-1">
        {elicitation.allowSkip !== false ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={isSubmitting}
            onClick={decline}
          >
            {t("elicitation.skip")}
          </Button>
        ) : null}
        <Button
          type="button"
          size="sm"
          disabled={isSubmitting}
          onClick={submit}
        >
          {isSubmitting ? <Loader2 className="animate-spin" /> : null}
          {t("elicitation.next")}
          {!isSubmitting ? <CornerDownLeft aria-hidden="true" /> : null}
        </Button>
      </div>
    </div>
  );
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
      <span
        aria-hidden="true"
        className="bg-muted text-muted-foreground type-ui size-icon-lg flex shrink-0 items-center justify-center rounded-xs"
      >
        {index < 26 ? String.fromCharCode(65 + index) : index + 1}
      </span>
      <span className="min-w-0 flex-1 text-left">
        <span className="block break-words whitespace-pre-wrap">{label}</span>
        {description ? (
          <span className="text-muted-foreground type-caption mt-0.5 block font-normal break-words whitespace-pre-wrap">
            {description}
          </span>
        ) : null}
      </span>
      {selected ? (
        <Check aria-hidden="true" className="mt-0.5 shrink-0" />
      ) : null}
    </Button>
  );
}
