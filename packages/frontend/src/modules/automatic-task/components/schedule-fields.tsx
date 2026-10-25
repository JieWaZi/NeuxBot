import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { CalendarDays, ChevronDown, Clock3, Repeat2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  previewAutomaticSchedule,
  previewGlobalAutomaticSchedule,
} from "@/shared/api/generated/sdk.gen";
import {
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { DatePicker, TimePicker } from "@/shared/ui/date-time-picker";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { withScheduleTimezone } from "../model/schedule-defaults";
import { Input } from "@/shared/ui/input";
import { FieldLabel } from "@/shared/ui/field-label";
import { TimeZonePicker } from "@/shared/ui/time-zone-picker";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import type { TaskSchedule } from "../model/types";
import { formatTime } from "./task-presentation";

export function SchedulePreview({
  wakerId,
  rule,
  compact = false,
  surface = "muted",
}: {
  wakerId?: string;
  rule: TaskSchedule;
  compact?: boolean;
  surface?: "muted" | "plain";
}) {
  const { t, i18n } = useTranslation("automaticTask");
  const schedule = withScheduleTimezone(rule);
  const ready = Boolean(
    rule.kind &&
    schedule.timezone &&
    (rule.kind === "once"
      ? rule.onceAt
      : rule.kind === "cron"
        ? rule.cron?.trim()
        : typeof rule.minute === "number" &&
          (rule.kind === "hourly" || typeof rule.hour === "number")),
  );
  const preview = useQuery({
    queryKey: ["automatic-task", "schedule-preview", wakerId, schedule],
    queryFn: async ({ signal }) => {
      const options = withLocalErrorSurface({
        body: schedule,
        signal,
        throwOnError: true as const,
      });
      const result = wakerId
        ? await previewAutomaticSchedule({ ...options, path: { wakerId } })
        : await previewGlobalAutomaticSchedule(options);
      return result.data;
    },
    retry: false,
    enabled: ready,
  });
  const Heading = surface === "plain" ? "h3" : "p";
  return (
    <div
      className={
        surface === "plain"
          ? "type-body min-w-0"
          : "bg-surface-tertiary type-ui rounded-md p-3"
      }
      aria-live="polite"
    >
      <Heading
        className={cn(
          "flex items-center gap-2 font-medium",
          surface === "plain" && "type-label",
        )}
      >
        {surface === "muted" ? (
          <Clock3 aria-hidden className="size-icon-sm text-content-secondary" />
        ) : null}
        {t("preview")}
      </Heading>
      {!ready ? (
        <p className="text-content-secondary mt-1">{t("previewHint")}</p>
      ) : preview.isPending ? (
        <p className="text-content-secondary mt-1">{t("common:pageLoading")}</p>
      ) : preview.isError ? (
        <p role="alert" className="text-destructive mt-1">
          {localApiErrorMessage(preview.error, t("actionFailed"))}
        </p>
      ) : (
        <>
          {preview.data?.nextRuns?.length ? (
            <ul className="text-content-secondary mt-1 space-y-1">
              {preview.data.nextRuns
                .slice(0, compact ? 1 : undefined)
                .map((value) => (
                  <li key={value}>
                    {formatTime(value, schedule.timezone, i18n.language)}
                  </li>
                ))}
            </ul>
          ) : (
            <p className="text-content-secondary mt-1">{t("noFuture")}</p>
          )}
          {(preview.data?.runsIn24Hours ?? 0) >= 24 ? (
            <p className="text-warning mt-2">
              {t("highFrequency", { count: preview.data?.runsIn24Hours })}
            </p>
          ) : null}
        </>
      )}
    </div>
  );
}

export function ScheduleFields({
  wakerId,
  rule,
  onChange,
  compact = false,
}: {
  wakerId?: string;
  rule: TaskSchedule;
  onChange: (value: TaskSchedule) => void;
  compact?: boolean;
}) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const schedule = withScheduleTimezone(rule);
  const set = (values: Partial<TaskSchedule>) =>
    onChange(withScheduleTimezone({ ...rule, ...values }));
  const recurringKinds = [
    "hourly",
    "daily",
    "weekly",
    "monthly",
    "cron",
  ] as const;
  const once = rule.kind === "once";
  const time =
    typeof rule.hour === "number" && typeof rule.minute === "number"
      ? `${String(rule.hour).padStart(2, "0")}:${String(rule.minute).padStart(2, "0")}`
      : "";
  const timezone = (
    <Collapsible className="grid gap-2">
      <CollapsibleTrigger asChild>
        <Button
          size="sm"
          variant="ghost"
          className="group justify-self-start"
          aria-label={t("timezone")}
        >
          <span>{t("timezone")}</span>
          <span className="text-content-secondary">{schedule.timezone}</span>
          <ChevronDown
            aria-hidden
            className="transition-transform group-data-[state=open]:rotate-180 motion-reduce:transition-none"
          />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <TimeZonePicker
          id={`${id}-timezone`}
          aria-label={t("timezone")}
          value={schedule.timezone ?? ""}
          onChange={(timezone) => set({ timezone })}
        />
      </CollapsibleContent>
    </Collapsible>
  );
  const numeric = (
    key: "hour" | "minute" | "dayOfMonth",
    min: number,
    max: number,
  ) => (
    <div className="grid gap-2">
      <FieldLabel
        required
        htmlFor={`${id}-${key}`}
        className="type-ui font-medium"
      >
        {t(key)}
      </FieldLabel>
      <Input
        id={`${id}-${key}`}
        aria-label={t(key)}
        type="number"
        min={min}
        max={max}
        required
        value={rule[key] ?? min}
        onChange={(e) => set({ [key]: Number(e.target.value) })}
      />
    </div>
  );
  return (
    <div className="grid gap-4">
      <div className="grid gap-2">
        <FieldLabel required>{t("scheduleType")}</FieldLabel>
        <SegmentedControl
          ariaLabel={t("scheduleType")}
          value={once ? "once" : "recurring"}
          options={[
            { value: "recurring", label: t("recurring"), icon: <Repeat2 /> },
            { value: "once", label: t("kinds.once"), icon: <CalendarDays /> },
          ]}
          onValueChange={(mode) =>
            set({ kind: mode === "once" ? "once" : "daily" })
          }
        />
      </div>
      {once ? (
        <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-4">
          <div className="grid gap-2">
            <FieldLabel
              required
              htmlFor={`${id}-once`}
              className="type-ui font-medium"
            >
              {t("onceAt")}
            </FieldLabel>
            <div className="grid grid-cols-2 gap-3">
              <DatePicker
                required
                id={`${id}-once`}
                aria-label={t("onceAt")}
                value={rule.onceAt?.split("T")[0] ?? ""}
                onChange={(date) =>
                  set({
                    onceAt: `${date}T${rule.onceAt?.split("T")[1] || "09:00"}`,
                  })
                }
              />
              <TimePicker
                required
                aria-label={t("runAt")}
                value={rule.onceAt?.split("T")[1] || "09:00"}
                onChange={(time) => {
                  const date = rule.onceAt?.split("T")[0];
                  if (date) set({ onceAt: `${date}T${time}` });
                }}
                disabled={!rule.onceAt}
              />
            </div>
          </div>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-4">
            <div className="grid gap-2">
              <FieldLabel
                required
                htmlFor={`${id}-kind`}
                className="type-ui font-medium"
              >
                {t("repeat")}
              </FieldLabel>
              <Select
                value={rule.kind}
                onValueChange={(value) => {
                  const kind = recurringKinds.find((kind) => kind === value);
                  if (kind) set({ kind });
                }}
              >
                <SelectTrigger
                  id={`${id}-kind`}
                  aria-label={t("repeat")}
                  aria-required
                  size="md"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {recurringKinds.map((kind) => (
                    <SelectItem key={kind} value={kind}>
                      {t(`kinds.${kind}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {rule.kind === "cron" ? null : rule.kind === "hourly" ? (
              numeric("minute", 0, 59)
            ) : (
              <div className="grid gap-2">
                <FieldLabel
                  required
                  htmlFor={`${id}-time`}
                  className="type-ui font-medium"
                >
                  {t("runAt")}
                </FieldLabel>
                <TimePicker
                  required
                  id={`${id}-time`}
                  aria-label={t("runAt")}
                  value={time}
                  onChange={(value) => {
                    const [hour, minute] = value.split(":").map(Number);
                    set({ hour, minute });
                  }}
                />
              </div>
            )}
          </div>
          {rule.kind === "cron" ? (
            <div className="grid gap-2">
              <FieldLabel
                required
                htmlFor={`${id}-cron`}
                className="type-ui font-medium"
              >
                {t("cron")}
              </FieldLabel>
              <Input
                id={`${id}-cron`}
                aria-label={t("cron")}
                required
                maxLength={120}
                placeholder="0 9 * * 1-5"
                value={rule.cron ?? ""}
                onChange={(e) => set({ cron: e.target.value })}
              />
              <p className="text-content-secondary type-caption">
                {t("cronHint")}
              </p>
            </div>
          ) : rule.kind === "monthly" || rule.kind === "weekly" ? (
            <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-4">
              {rule.kind === "monthly" && numeric("dayOfMonth", 1, 31)}
              {rule.kind === "weekly" ? (
                <div className="grid gap-2">
                  <FieldLabel
                    required
                    htmlFor={`${id}-weekday`}
                    className="type-ui font-medium"
                  >
                    {t("weekday")}
                  </FieldLabel>
                  <Select
                    value={String(rule.weekday ?? 1)}
                    onValueChange={(day) => set({ weekday: Number(day) })}
                  >
                    <SelectTrigger
                      id={`${id}-weekday`}
                      aria-label={t("weekday")}
                      aria-required
                      size="md"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {Array.from({ length: 7 }, (_, day) => (
                        <SelectItem key={day} value={String(day)}>
                          {t(`weekdays.${day}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
            </div>
          ) : null}
          {rule.kind === "monthly" ? (
            <p className="text-content-secondary type-caption">
              {t("monthHint")}
            </p>
          ) : null}
        </>
      )}
      {timezone}
      <SchedulePreview compact={compact} wakerId={wakerId} rule={rule} />
      <p className="text-content-secondary type-caption">{t("scheduleHint")}</p>
    </div>
  );
}
