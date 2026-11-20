import type { ReactNode } from "react";
import { Check, CornerDownLeft, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { Button } from "./button";

/** 聊天中的交互问题共享卡片和操作区，提交仍由原问题的业务入口负责。 */
export function QuestionCard({
  id,
  busy,
  children,
}: {
  id: string;
  busy?: boolean;
  children: ReactNode;
}) {
  return (
    <article
      data-chat-elicitation-card
      data-message-id={id}
      aria-busy={busy || undefined}
      className="bg-message-card-background max-w-chat-question w-full space-y-3 rounded-2xl px-4 py-3"
    >
      {children}
    </article>
  );
}

export function QuestionResultHeader({
  children,
  status,
}: {
  children?: ReactNode;
  status?: string;
}) {
  return (
    <div className="flex items-start gap-3">
      {children}
      {status ? (
        <span className="text-muted-foreground type-ui ms-auto shrink-0 whitespace-nowrap">
          {status}
        </span>
      ) : null}
    </div>
  );
}

export function QuestionActions({
  canSubmit,
  disabled,
  submitting,
  onSkip,
  onSubmit,
}: {
  canSubmit: boolean;
  disabled: boolean;
  submitting: boolean;
  onSkip?: () => void;
  onSubmit: () => void;
}) {
  const { t } = useTranslation("chat");
  return (
    <div className="flex flex-wrap justify-end gap-2 pt-1">
      {onSkip && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={disabled}
          onClick={onSkip}
        >
          {t("elicitation.skip")}
        </Button>
      )}
      <Button
        type="button"
        size="sm"
        disabled={disabled || !canSubmit}
        onClick={onSubmit}
      >
        {submitting && <Loader2 className="animate-spin" />}
        {t("elicitation.next")}
        {!submitting && <CornerDownLeft aria-hidden="true" />}
      </Button>
    </div>
  );
}

export function QuestionChoice({
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
      <QuestionOptionLetter index={index} selected={selected} />
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

export function QuestionOptionLetter({
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
