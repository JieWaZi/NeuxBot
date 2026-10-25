import { Check, ChevronDown, Copy } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import {
  integrationServiceOptions,
  integrationServiceMutation,
  integrationServiceQueryKey,
} from "@/shared/api/integration-service";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { Input } from "@/shared/ui/input";
import { QueryState } from "@/shared/ui/query-state";
import { Skeleton } from "@/shared/ui/skeleton";
import { Switch } from "@/shared/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useIntegrationCopy } from "../use-integration-copy";
import { SettingsFieldGroup, SettingsFieldRow } from "./settings-field-group";

export function APIServiceSettings() {
  const t = useIntegrationCopy();
  const id = useId();
  const client = useQueryClient();
  const service = useQuery(integrationServiceOptions());
  const [copied, setCopied] = useState(false);
  const [configOpen, setConfigOpen] = useState(false);
  const [draft, setDraft] = useState<{
    ip?: string;
    port?: string;
    enabled?: boolean;
    publicURL?: string;
  }>({});
  const ip = draft.ip ?? service.data?.ip ?? "";
  const port = draft.port ?? String(service.data?.port || "");
  const enabled = draft.enabled ?? service.data?.enabled ?? false;
  const publicURL = draft.publicURL ?? service.data?.publicBaseUrl ?? "";
  const save = useMutation({
    ...integrationServiceMutation(),
    onSuccess: async () => {
      setDraft({});
      setCopied(false);
      await client.invalidateQueries({
        queryKey: integrationServiceQueryKey(),
      });
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate({
      body: {
        ip: ip.trim(),
        port: Number(port),
        enabled,
        publicBaseUrl: publicURL.trim(),
      },
    });
  }
  const invalid =
    !ip.trim() ||
    !Number.isInteger(Number(port)) ||
    Number(port) < 1 ||
    Number(port) > 65535;
  const base = service.data?.publicBaseUrl ?? "";
  const state = !service.data?.enabled
    ? "disabled"
    : service.data.available
      ? "available"
      : "unavailable";
  return (
    <Card asChild variant="section">
      <section data-od-id="api-service">
        {service.isPending ? (
          <div role="status" aria-busy="true" className="grid gap-3">
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-row-default w-full" />
            <Skeleton className="h-row-default w-full" />
          </div>
        ) : service.isError && !service.data ? (
          <QueryState
            error={service.error}
            onRetry={() => void service.refetch()}
          />
        ) : (
          <form className="grid gap-5" onSubmit={submit}>
            <div
              role="status"
              aria-label={t("serviceStatus")}
              className="grid min-w-0 gap-5"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 className="type-label font-medium">{t("service")}</h2>
                </div>
                <Badge
                  variant={
                    state === "available"
                      ? "success"
                      : state === "unavailable"
                        ? "destructive"
                        : "secondary"
                  }
                >
                  {t(
                    state === "available"
                      ? "statusAvailable"
                      : state === "unavailable"
                        ? "statusUnavailable"
                        : "statusDisabled",
                  )}
                </Badge>
              </div>
              <div className="bg-surface-scene flex min-w-0 items-center gap-4 rounded-lg px-4 py-2">
                <span
                  id={`${id}-current-url-label`}
                  className="type-ui text-content-secondary shrink-0"
                >
                  {t("publicURL")}
                </span>
                {base ? (
                  <>
                    <p
                      aria-labelledby={`${id}-current-url-label`}
                      className="type-body min-w-0 flex-1 truncate select-text"
                      title={base}
                    >
                      {base}
                    </p>
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            size="icon"
                            variant="ghost"
                            className="shrink-0"
                            aria-label={t(
                              copied ? "copiedAddress" : "copyAddress",
                            )}
                            onClick={async () => {
                              await navigator.clipboard.writeText(base);
                              setCopied(true);
                            }}
                          >
                            {copied ? (
                              <Check aria-hidden="true" />
                            ) : (
                              <Copy aria-hidden="true" />
                            )}
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>
                          {t(copied ? "copiedAddress" : "copyAddress")}
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  </>
                ) : (
                  <p className="type-body text-content-secondary">
                    {t("noPublicURL")}
                  </p>
                )}
              </div>
            </div>

            <Collapsible open={configOpen} onOpenChange={setConfigOpen}>
              <div className="border-border-subtle flex flex-wrap items-center justify-between gap-2 border-t pt-4">
                <p className="type-ui text-content-secondary">
                  {t("serviceHelp")}
                </p>
                <CollapsibleTrigger asChild>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    data-od-id="api-connection-settings"
                  >
                    {t("connectionSettings")}
                    <ChevronDown
                      aria-hidden="true"
                      className={configOpen ? "rotate-180" : undefined}
                    />
                  </Button>
                </CollapsibleTrigger>
              </div>
              <CollapsibleContent>
                <div className="grid gap-4 pt-4">
                  <SettingsFieldGroup description={t("listenerSettingsHelp")}>
                    <SettingsFieldRow
                      label={t("enabled")}
                      htmlFor={`${id}-enabled`}
                    >
                      <Switch
                        id={`${id}-enabled`}
                        checked={enabled}
                        onClick={() =>
                          setDraft({ ...draft, enabled: !enabled })
                        }
                        disabled={save.isPending}
                      />
                    </SettingsFieldRow>
                    <SettingsFieldRow label={t("ip")} htmlFor={`${id}-ip`}>
                      <div className="grid gap-2">
                        <Input
                          id={`${id}-ip`}
                          value={ip}
                          placeholder="127.0.0.1"
                          onChange={(event) =>
                            setDraft({ ...draft, ip: event.target.value })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("ipHelp")}
                        </p>
                      </div>
                    </SettingsFieldRow>
                    <SettingsFieldRow label={t("port")} htmlFor={`${id}-port`}>
                      <div className="grid gap-2">
                        <Input
                          id={`${id}-port`}
                          type="number"
                          value={port}
                          min={1}
                          max={65535}
                          placeholder="8431"
                          onChange={(event) =>
                            setDraft({ ...draft, port: event.target.value })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("portHelp")}
                        </p>
                      </div>
                    </SettingsFieldRow>
                    <SettingsFieldRow
                      label={t("publicURL")}
                      htmlFor={`${id}-url`}
                    >
                      <div className="grid gap-2">
                        <Input
                          id={`${id}-url`}
                          value={publicURL}
                          placeholder={t("publicURLPlaceholder")}
                          onChange={(event) =>
                            setDraft({
                              ...draft,
                              publicURL: event.target.value,
                            })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("publicURLHelp")}
                        </p>
                      </div>
                    </SettingsFieldRow>
                  </SettingsFieldGroup>

                  <div className="flex flex-wrap items-center justify-end gap-3">
                    <Button type="submit" disabled={invalid || save.isPending}>
                      {t(save.isPending ? "saving" : "save")}
                    </Button>
                  </div>
                </div>
              </CollapsibleContent>
            </Collapsible>
            {(save.error || service.error) && (
              <p role="alert" className="type-body text-destructive">
                {localApiErrorMessage(save.error || service.error, t("failed"))}
              </p>
            )}
          </form>
        )}
      </section>
    </Card>
  );
}
