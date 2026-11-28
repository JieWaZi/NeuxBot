import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { FieldLabel } from "@/shared/ui/field-label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import type { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import type { TaskConfiguration } from "../model/types";

export type WorkflowReportRecipient = NonNullable<
  TaskConfiguration["workflowReportRecipient"]
>;
type WorkExecutors = ReturnType<typeof useWorkExecutors>;

function recipientKey(recipient?: WorkflowReportRecipient) {
  return recipient ? `${recipient.subjectType}:${recipient.subjectId}` : "";
}

export function workflowReportOptions(directory: WorkExecutors["data"]) {
  return [
    ...(directory?.wakers ?? []).flatMap((waker) =>
      waker.id && waker.name
        ? [
            {
              subjectType: "waker" as const,
              subjectId: waker.id,
              name: waker.name,
            },
          ]
        : [],
    ),
    ...(directory?.teams ?? []).flatMap(({ channel }) =>
      channel?.id && channel.name
        ? [
            {
              subjectType: "team" as const,
              subjectId: channel.id,
              name: channel.name,
            },
          ]
        : [],
    ),
  ];
}

export function WorkflowReportFields({
  enabled,
  recipient,
  options,
  directory,
  onToggle,
  onSelect,
}: {
  enabled: boolean;
  recipient?: WorkflowReportRecipient;
  options: ReturnType<typeof workflowReportOptions>;
  directory: WorkExecutors;
  onToggle: () => void;
  onSelect: (recipient: WorkflowReportRecipient) => void;
}) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const selected = options.find(
    (option) => recipientKey(option) === recipientKey(recipient),
  );
  return (
    <div className="grid gap-3">
      <div className="flex items-center justify-between gap-3">
        <FieldLabel htmlFor={`${id}-report`}>
          {t("workflowReportEnabled")}
        </FieldLabel>
        <Switch
          id={`${id}-report`}
          hitArea="comfortable"
          checked={enabled}
          onClick={onToggle}
        />
      </div>
      {enabled && (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${id}-recipient`} required>
            {t("workflowReportRecipient")}
          </FieldLabel>
          <Select
            value={recipientKey(selected)}
            disabled={directory.isPending || options.length === 0}
            onValueChange={(value) => {
              const option = options.find(
                (option) => recipientKey(option) === value,
              );
              if (option) {
                const { subjectType, subjectId } = option;
                onSelect({ subjectType, subjectId });
              }
            }}
          >
            <SelectTrigger id={`${id}-recipient`} width="full">
              <SelectValue placeholder={t("chooseWorkflowReportRecipient")} />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem
                  key={recipientKey(option)}
                  value={recipientKey(option)}
                >
                  {options.some(
                    (other) => other !== option && other.name === option.name,
                  )
                    ? `${option.name} · ${t(option.subjectType === "team" ? "teamExecutor" : "partnerExecutor")}`
                    : option.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {directory.isError && (
            <Button
              type="button"
              size="sm"
              variant="link"
              onClick={() => void directory.refetch()}
            >
              {t("retry")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
