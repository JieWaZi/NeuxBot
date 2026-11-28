import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { listAutomaticEventSourcesOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { QueryState } from "@/shared/ui/query-state";
import { Checkbox } from "@/shared/ui/checkbox";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Skeleton } from "@/shared/ui/skeleton";
import { Textarea } from "@/shared/ui/textarea";
import type { TaskConfiguration } from "../model/types";

export type EventConfiguration = NonNullable<TaskConfiguration["event"]>;

export const defaultEventConfiguration: EventConfiguration = {
  source: "github_pull_requests",
  repository: "",
  kinds: ["pull_opened"],
  excludeDrafts: true,
};

export function EventFields({
  value,
  onChange,
  canTrackDiscussion,
}: {
  value: EventConfiguration;
  onChange: (value: EventConfiguration) => void;
  canTrackDiscussion: boolean;
}) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const catalog = useQuery(listAutomaticEventSourcesOptions());
  if (catalog.isPending)
    return (
      <div role="status" aria-busy="true" className="grid gap-3">
        <Skeleton className="h-control-md w-full" />
        <Skeleton className="h-control-md w-full" />
      </div>
    );
  if (catalog.isError)
    return (
      <QueryState
        error={catalog.error}
        onRetry={() => void catalog.refetch()}
      />
    );

  const source = catalog.data.resources?.find(({ id }) => id === value.source);
  if (!source) return <p role="alert">{t("eventSourceUnavailable")}</p>;
  const fields = new Set(source.fields);
  const set = (patch: Partial<EventConfiguration>) =>
    onChange({ ...value, ...patch });

  return (
    <div className="grid gap-4">
      <div className="type-ui font-medium">{t("eventSourceGitHub")}</div>
      {fields.has("repository") ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${id}-repository`}>
            {t("eventRepository")}
          </FieldLabel>
          <Input
            id={`${id}-repository`}
            value={value.repository}
            required
            placeholder="owner/repo"
            maxLength={250}
            onChange={(event) => set({ repository: event.target.value })}
          />
        </div>
      ) : null}
      {fields.has("kinds") ? (
        <fieldset className="grid gap-2">
          <legend className="type-ui mb-2 font-medium">
            {t("eventTypes")}
          </legend>
          {source.kinds?.map((kind) => (
            <label key={kind} className="type-ui flex items-center gap-2">
              <Checkbox
                checked={Boolean(value.kinds?.includes(kind))}
                onCheckedChange={(checked) => {
                  const retained = value.kinds ?? [];
                  set({
                    kinds: checked
                      ? [...retained, kind]
                      : retained.filter((item) => item !== kind),
                  });
                }}
              />
              {t(`eventKind_${kind}`, { defaultValue: kind })}
            </label>
          ))}
        </fieldset>
      ) : null}
      {fields.has("excludeDrafts") ? (
        <label className="type-ui flex items-center gap-2">
          <Checkbox
            checked={Boolean(value.excludeDrafts)}
            onCheckedChange={(checked) =>
              set({ excludeDrafts: checked === true })
            }
          />
          {t("eventExcludeDrafts")}
        </label>
      ) : null}
      {fields.has("keyword") ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${id}-keyword`}>{t("eventKeyword")}</FieldLabel>
          <Input
            id={`${id}-keyword`}
            maxLength={120}
            value={value.keyword ?? ""}
            onChange={(event) => set({ keyword: event.target.value })}
          />
        </div>
      ) : null}
      {fields.has("semanticCondition") ? (
        <div className="grid gap-2">
          <FieldLabel htmlFor={`${id}-condition`}>
            {t("eventSemanticCondition")}
          </FieldLabel>
          <Textarea
            id={`${id}-condition`}
            maxLength={500}
            rows={2}
            value={value.semanticCondition ?? ""}
            onChange={(event) => set({ semanticCondition: event.target.value })}
          />
        </div>
      ) : null}
      {canTrackDiscussion && fields.has("trackDiscussion") ? (
        <label className="type-ui flex items-center gap-2">
          <Checkbox
            checked={Boolean(value.trackDiscussion)}
            onCheckedChange={(checked) =>
              set({ trackDiscussion: checked === true })
            }
          />
          {t("eventTrackDiscussion")}
        </label>
      ) : null}
    </div>
  );
}
