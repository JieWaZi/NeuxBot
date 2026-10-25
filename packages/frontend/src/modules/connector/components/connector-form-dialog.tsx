import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createWakerConnectorMutation,
  invalidateVisibleConnectorQueries,
  updateWakerConnectorMutation,
} from "../api/connector-queries";
import {
  connectorTrustFieldsChanged,
  parseArguments,
  toConnectorFormValue,
} from "../model/connector-view";
import {
  type ConnectorFormValue,
  type ConnectorRecord,
  type SecretField,
} from "../model/types";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  connector?: ConnectorRecord;
  onOpenChange: (open: boolean) => void;
  onSaved?: (connectorId: string) => void;
  open: boolean;
  wakerId: string;
};

export function ConnectorFormDialog(props: Props) {
  if (!props.open) return null;
  return <OpenConnectorFormDialog {...props} />;
}

function OpenConnectorFormDialog({
  connector,
  onOpenChange,
  onSaved,
  open,
  wakerId,
}: Props) {
  const { t } = useTranslation("connector");
  const queryClient = useQueryClient();
  const initialValue = useMemo(
    () => toConnectorFormValue(connector),
    [connector],
  );
  const [form, setForm] = useState<ConnectorFormValue>(initialValue);
  const [trustConfirmed, setTrustConfirmed] = useState(false);
  const [localError, setLocalError] = useState("");
  const createConnector = useMutation(
    withLocalErrorMutation(createWakerConnectorMutation()),
  );
  const updateConnector = useMutation(
    withLocalErrorMutation(updateWakerConnectorMutation()),
  );
  const builtIn = connector?.scope === "built_in";
  const trustChanged =
    !builtIn &&
    connectorTrustFieldsChanged(connector ? initialValue : undefined, form);
  const pending = createConnector.isPending || updateConnector.isPending;

  const save = async () => {
    const normalizedName = form.name.trim();
    const timeoutMS = Number(form.timeoutMS);
    if (!normalizedName) {
      setLocalError(t("form.nameRequired"));
      return;
    }
    if (!Number.isInteger(timeoutMS) || timeoutMS < 100 || timeoutMS > 300000) {
      setLocalError(t("form.timeoutInvalid"));
      return;
    }
    if (form.transport === "stdio" && !form.command.trim()) {
      setLocalError(t("form.commandRequired"));
      return;
    }
    if (form.transport !== "stdio" && !form.url.trim()) {
      setLocalError(t("form.urlRequired"));
      return;
    }
    const secrets =
      form.transport === "stdio" ? form.environment : form.headers;
    if (secrets.some((item) => !item.name?.trim())) {
      setLocalError(t("form.secretNameRequired"));
      return;
    }
    let commandArguments: string[];
    try {
      commandArguments =
        form.transport === "stdio" ? parseArguments(form.argumentsText) : [];
    } catch {
      setLocalError(t("form.argumentsInvalid"));
      return;
    }
    if (trustChanged && !trustConfirmed) {
      setLocalError(t("form.trustRequired"));
      return;
    }

    const body = {
      arguments: commandArguments,
      command: form.transport === "stdio" ? form.command.trim() : "",
      description: form.description.trim(),
      environment:
        form.transport === "stdio" ? toSecretInputs(form.environment) : [],
      headers: form.transport === "stdio" ? [] : toSecretInputs(form.headers),
      name: normalizedName,
      timeoutMs: timeoutMS,
      transport: form.transport,
      trustConfirmed: trustChanged ? trustConfirmed : false,
      url: form.transport === "stdio" ? "" : form.url.trim(),
    };
    try {
      const result = connector?.id
        ? await updateConnector.mutateAsync({
            path: { connectorId: connector.id, wakerId },
            body,
          })
        : await createConnector.mutateAsync({ path: { wakerId }, body });
      await invalidateVisibleConnectorQueries(queryClient);
      onOpenChange(false);
      onSaved?.(result.connector?.id ?? connector?.id ?? "");
    } catch {
      // React Query 保存错误，由表单就近展示。
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={connector ? t("form.editTitle") : t("form.createTitle")}
        description={t("form.description")}
        size="lg"
        layout="form"
      >
        <div className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto px-6 py-2">
          <div className="grid gap-5">
            <FormField label={t("form.name")}>
              <Input
                value={form.name}
                disabled={builtIn}
                maxLength={120}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
              />
            </FormField>
            <FormField label={t("form.connectorDescription")}>
              <Textarea
                value={form.description}
                disabled={builtIn}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    description: event.target.value,
                  }))
                }
              />
            </FormField>
            <div className="grid gap-5 sm:grid-cols-2">
              <FormField label={t("form.transport")}>
                <Select
                  value={form.transport}
                  disabled={builtIn}
                  onValueChange={(value) =>
                    setForm((current) => ({
                      ...current,
                      transport: value as ConnectorFormValue["transport"],
                    }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="stdio">
                      {t("transport.stdio")}
                    </SelectItem>
                    <SelectItem value="streamable_http">
                      {t("transport.streamable_http")}
                    </SelectItem>
                    <SelectItem value="sse">{t("transport.sse")}</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
              <FormField label={t("form.timeout")}>
                <Input
                  type="number"
                  min={100}
                  max={300000}
                  value={form.timeoutMS}
                  disabled={builtIn}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      timeoutMS: event.target.value,
                    }))
                  }
                />
              </FormField>
            </div>

            {form.transport === "stdio" ? (
              <>
                <FormField label={t("form.command")}>
                  <Input
                    value={form.command}
                    disabled={builtIn}
                    placeholder="npx"
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        command: event.target.value,
                      }))
                    }
                  />
                </FormField>
                <FormField
                  label={t("form.arguments")}
                  help={t("form.argumentsHelp")}
                >
                  <Input
                    value={form.argumentsText}
                    disabled={builtIn}
                    placeholder="-y @modelcontextprotocol/server-example"
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        argumentsText: event.target.value,
                      }))
                    }
                  />
                </FormField>
                <SecretFields
                  builtIn={builtIn}
                  fields={form.environment}
                  label={t("form.environment")}
                  namePlaceholder="API_TOKEN"
                  onChange={(environment) =>
                    setForm((current) => ({ ...current, environment }))
                  }
                />
              </>
            ) : (
              <>
                <FormField label={t("form.url")}>
                  <Input
                    type="url"
                    value={form.url}
                    disabled={builtIn}
                    placeholder="https://example.com/mcp"
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        url: event.target.value,
                      }))
                    }
                  />
                </FormField>
                <SecretFields
                  builtIn={builtIn}
                  fields={form.headers}
                  label={t("form.headers")}
                  namePlaceholder="Authorization"
                  onChange={(headers) =>
                    setForm((current) => ({ ...current, headers }))
                  }
                />
              </>
            )}

            {trustChanged ? (
              <label className="border-success/40 bg-success/8 flex cursor-pointer gap-3 rounded-md border p-3">
                <Checkbox
                  className="data-[state=checked]:border-success data-[state=checked]:bg-success mt-0.5"
                  checked={trustConfirmed}
                  onCheckedChange={(checked) =>
                    setTrustConfirmed(checked === true)
                  }
                />
                <span>
                  <span className="type-label block font-medium">
                    {t("form.trustTitle")}
                  </span>
                  <span className="text-muted-foreground type-body mt-1 block">
                    {t("form.trustDescription")}
                  </span>
                </span>
              </label>
            ) : null}
            {builtIn ? (
              <p className="bg-muted text-muted-foreground type-body rounded-md p-3">
                {t("form.builtInHelp")}
              </p>
            ) : null}
            {localError ||
            createConnector.isError ||
            updateConnector.isError ? (
              <p role="alert" className="text-destructive type-body">
                {localError || t("form.saveFailed")}
              </p>
            ) : null}
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary" disabled={pending}>
              {t("common:cancel")}
            </Button>
          </DialogClose>
          <Button disabled={pending} onClick={() => void save()}>
            {pending ? t("form.saving") : t("common:save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SecretFields({
  builtIn,
  fields,
  label,
  namePlaceholder,
  onChange,
}: {
  builtIn: boolean;
  fields: SecretField[];
  label: string;
  namePlaceholder: string;
  onChange: (fields: SecretField[]) => void;
}) {
  const { t } = useTranslation("connector");
  const update = (index: number, patch: Partial<SecretField>) => {
    onChange(
      fields.map((field, current) =>
        current === index ? { ...field, ...patch } : field,
      ),
    );
  };
  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-3">
        <h3 className="type-label font-medium">{label}</h3>
        {!builtIn ? (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            onClick={() => onChange([...fields, { name: "", value: "" }])}
          >
            <Plus />
            {t("form.addSecret")}
          </Button>
        ) : null}
      </div>
      <div className="grid gap-2">
        {fields.length === 0 ? (
          <p className="border-border bg-muted/40 text-muted-foreground type-body rounded-md border border-dashed p-3">
            {t("form.noSecrets")}
          </p>
        ) : (
          fields.map((field, index) => (
            <div
              key={field.id ?? index}
              className="border-border bg-muted/40 grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_auto]"
            >
              <Input
                aria-label={t("form.secretName")}
                value={field.name ?? ""}
                disabled={builtIn}
                placeholder={namePlaceholder}
                onChange={(event) =>
                  update(index, { name: event.target.value })
                }
              />
              <Input
                aria-label={t("form.secretValue")}
                type="password"
                value={field.value ?? ""}
                placeholder={
                  field.clear
                    ? t("form.secretWillClear")
                    : field.configured
                      ? t("form.secretKeep")
                      : t("form.secretValuePlaceholder")
                }
                onChange={(event) =>
                  update(index, { clear: false, value: event.target.value })
                }
              />
              <div className="flex items-center gap-1">
                {field.configured ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() =>
                      update(index, { clear: !field.clear, value: "" })
                    }
                  >
                    {field.clear ? t("form.undoClear") : t("form.clearSecret")}
                  </Button>
                ) : null}
                {!builtIn ? (
                  <Button
                    type="button"
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t("form.removeSecret")}
                    onClick={() =>
                      onChange(fields.filter((_, current) => current !== index))
                    }
                  >
                    <Trash2 />
                  </Button>
                ) : null}
              </div>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

function FormField({
  children,
  help,
  label,
}: {
  children: React.ReactNode;
  help?: string;
  label: string;
}) {
  return (
    <label className="grid gap-2">
      <span className="type-label font-medium">{label}</span>
      {children}
      {help ? (
        <span className="text-muted-foreground type-caption">{help}</span>
      ) : null}
    </label>
  );
}

function toSecretInputs(fields: SecretField[]) {
  return fields.map((field) => ({
    clear: field.clear ?? false,
    id: field.id,
    name: field.name?.trim() ?? "",
    value: field.value ?? "",
  }));
}
