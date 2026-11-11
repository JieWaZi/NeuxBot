import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Plus, Trash2 } from "lucide-react";
import {
  cloneElement,
  useId,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import {
  createWakerConnectorMutation,
  invalidateVisibleConnectorQueries,
  updateWakerConnectorMutation,
} from "../api/connector-queries";
import {
  connectorTrustFieldsChanged,
  connectorTimeoutMsFromSeconds,
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
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Input } from "@/shared/ui/input";
import { ResourcePageFooter } from "@/shared/ui/resource-page";
import { ResourceCategoryField } from "@/shared/ui/resource-category-field";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  connector?: ConnectorRecord;
  onOpenChange: (open: boolean) => void;
  onSaved?: (connectorId: string) => void;
  open: boolean;
  wakerId: string;
};

export function ConnectorFormDialog({ open, onOpenChange, ...props }: Props) {
  const { t } = useTranslation("connector");
  if (!open) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={props.connector ? t("form.editTitle") : t("form.createTitle")}
        size="lg"
        layout="form"
      >
        <ConnectorForm
          {...props}
          appearance="dialog"
          onCancel={() => onOpenChange(false)}
          onSaved={(id) => {
            onOpenChange(false);
            props.onSaved?.(id);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

/** 新建页与详情编辑使用同一配置及敏感值提交逻辑。 */
export function ConnectorForm({
  connector,
  onSaved,
  onCancel,
  wakerId,
  appearance = "page",
  renderHeader,
}: {
  connector?: ConnectorRecord;
  onSaved?: (connectorId: string) => void;
  onCancel?: () => void;
  wakerId: string;
  appearance?: "page" | "dialog";
  renderHeader?: (transportControl: ReactNode) => ReactNode;
}) {
  const { t } = useTranslation("connector");
  const queryClient = useQueryClient();
  const formId = useId();
  const initialValue = toConnectorFormValue(connector);
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
    setLocalError("");
    const normalizedName = form.name.trim();
    const timeoutMs = connectorTimeoutMsFromSeconds(form.timeoutSeconds);
    if (!normalizedName) {
      setLocalError(t("form.nameRequired"));
      return;
    }
    if (timeoutMs === null) {
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
      categories: form.categories,
      environment:
        form.transport === "stdio" ? toSecretInputs(form.environment) : [],
      headers: form.transport === "stdio" ? [] : toSecretInputs(form.headers),
      name: normalizedName,
      timeoutMs,
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
      onSaved?.(result.connector?.id ?? connector?.id ?? "");
    } catch {
      // React Query 保存错误，由表单就近展示。
    }
  };

  const error =
    localError ||
    (createConnector.error || updateConnector.error
      ? localApiErrorMessage(
          createConnector.error ?? updateConnector.error,
          t("form.saveFailed"),
        )
      : "");
  const dirty =
    JSON.stringify(form) !== JSON.stringify(initialValue) || trustConfirmed;
  const reset = () => {
    setForm(toConnectorFormValue(connector));
    setTrustConfirmed(false);
    setLocalError("");
    createConnector.reset();
    updateConnector.reset();
  };
  const transportControl = (
    <SegmentedControl
      appearance="catalog-form"
      size="sm"
      ariaLabel={t("form.transport")}
      value={form.transport}
      disabled={builtIn || pending}
      options={
        [
          { value: "stdio", label: t("transport.stdio") },
          {
            value: "streamable_http",
            label: t("transport.streamable_http"),
          },
          { value: "sse", label: t("transport.sse") },
        ] as const
      }
      onValueChange={(transport) =>
        setForm((current) => ({ ...current, transport }))
      }
    />
  );
  return (
    <form
      data-slot="connector-form"
      id={formId}
      className="flex min-h-0 min-w-0 flex-1 flex-col gap-6 overflow-hidden"
      onSubmit={(event) => {
        event.preventDefault();
        if (!pending) void save();
      }}
    >
      {renderHeader?.(transportControl)}
      <div
        data-slot="connector-form-content"
        className={
          appearance === "dialog"
            ? "scrollbar-hidden min-h-0 flex-1 overflow-y-auto px-6 py-2"
            : "scrollbar-hidden min-h-0 min-w-0 flex-1 overflow-y-auto pb-2"
        }
      >
        <div className="grid min-w-0 gap-5">
          {!renderHeader && transportControl}
          <FormField label={t("form.name")} help={t("form.nameHelp")}>
            <Input
              appearance="catalog"
              value={form.name}
              disabled={builtIn || pending}
              placeholder={t("form.namePlaceholder")}
              maxLength={120}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
            />
          </FormField>
          <FormField
            label={t("form.connectorDescription")}
            help={t("form.descriptionHelp")}
          >
            <Textarea
              appearance="catalog"
              value={form.description}
              placeholder={t("form.descriptionPlaceholder")}
              disabled={builtIn || pending}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  description: event.target.value,
                }))
              }
            />
          </FormField>
          <ResourceCategoryField
            type="connector"
            value={form.categories}
            onValueChange={(categories) =>
              setForm((current) => ({ ...current, categories }))
            }
            disabled={pending}
          />
          {connector && (
            <FormField label={t("form.timeout")} help={t("form.timeoutHelp")}>
              <Input
                appearance="catalog"
                type="number"
                placeholder="10"
                min={0.1}
                max={300}
                step={0.001}
                value={form.timeoutSeconds}
                disabled={builtIn || pending}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    timeoutSeconds: event.target.value,
                  }))
                }
              />
            </FormField>
          )}

          {form.transport === "stdio" ? (
            <>
              <FormField label={t("form.command")} help={t("form.commandHelp")}>
                <Input
                  appearance="catalog"
                  value={form.command}
                  disabled={builtIn || pending}
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
                  appearance="catalog"
                  value={form.argumentsText}
                  disabled={builtIn || pending}
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
                pending={pending}
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
              <FormField label={t("form.url")} help={t("form.urlHelp")}>
                <Input
                  appearance="catalog"
                  type="url"
                  value={form.url}
                  disabled={builtIn || pending}
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
                pending={pending}
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
            <label className="bg-resource-surface flex cursor-pointer gap-3 rounded-lg p-4">
              <Checkbox
                disabled={pending}
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
        </div>
      </div>
      {(dirty || error || appearance === "dialog") && (
        <ResourcePageFooter
          appearance={appearance}
          data-slot="connector-form-footer"
          aria-label={t("form.saveStatus")}
        >
          {(dirty || error) && (
            <p
              role={error ? "alert" : "status"}
              className={
                error
                  ? "text-destructive type-caption"
                  : "text-resource-muted type-caption"
              }
            >
              {error || t("form.unsaved")}
            </p>
          )}
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Button
              type="button"
              size="catalog-form"
              variant="catalog-ghost"
              disabled={pending}
              onClick={appearance === "dialog" ? onCancel : reset}
            >
              {appearance === "dialog" ? t("common:cancel") : t("form.reset")}
            </Button>
            <Button
              type="submit"
              form={formId}
              size="catalog-form"
              loading={pending}
            >
              {pending ? t("form.saving") : t("common:save")}
            </Button>
          </div>
        </ResourcePageFooter>
      )}
    </form>
  );
}

function SecretFields({
  builtIn,
  pending,
  fields,
  label,
  namePlaceholder,
  onChange,
}: {
  builtIn: boolean;
  pending: boolean;
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
        <h3 className="type-resource-caption text-resource-secondary font-medium">
          {label}
        </h3>
        {!builtIn ? (
          <Button
            type="button"
            size="catalog-form"
            variant="catalog-outline"
            disabled={pending}
            onClick={() => onChange([...fields, { name: "", value: "" }])}
          >
            <Plus />
            {t("form.addSecret")}
          </Button>
        ) : null}
      </div>
      <div className="grid gap-2">
        {fields.length === 0 ? (
          <p className="bg-resource-surface text-resource-muted type-body rounded-lg p-3">
            {t("form.noSecrets")}
          </p>
        ) : (
          fields.map((field, index) => (
            <div
              key={field.id ?? index}
              className="bg-resource-surface grid min-w-0 gap-2 rounded-lg p-3 @min-[32rem]/resource-page:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_auto]"
            >
              <Input
                appearance="catalog"
                aria-label={t("form.secretName")}
                value={field.name ?? ""}
                disabled={builtIn || pending}
                placeholder={namePlaceholder}
                onChange={(event) =>
                  update(index, { name: event.target.value })
                }
              />
              <Input
                appearance="catalog"
                aria-label={t("form.secretValue")}
                type="password"
                disabled={pending}
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
                    disabled={pending}
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
                    disabled={pending}
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
  children: ReactElement<{ id?: string; "aria-describedby"?: string }>;
  help?: string;
  label: string;
}) {
  const id = useId();
  const helpId = help ? `${id}-help` : undefined;
  return (
    <div className="grid gap-1.5">
      <label
        htmlFor={id}
        className="type-resource-caption text-resource-secondary font-medium"
      >
        {label}
      </label>
      {cloneElement(children, { id, "aria-describedby": helpId })}
      {help && (
        <p id={helpId} className="text-resource-muted type-resource-caption">
          {help}
        </p>
      )}
    </div>
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
