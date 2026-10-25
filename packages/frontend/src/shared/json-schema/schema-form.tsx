import type { Spec } from "@json-render/core";
import { InputRenderer } from "@/shared/presentation/input-renderer";
import type { PageDefinition } from "@/shared/presentation/catalog";
import {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useId,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Checkbox } from "@/shared/ui/checkbox";
import { Switch } from "@/shared/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/shared/ui/radio-group";
import { SchemaFileField } from "./schema-file-field";

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value))
    : undefined;
}
function jsonText(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? "null";
}

type Schema = Record<string, unknown>;

type ValidityChange = (path: string, valid: boolean) => void;

export function useSchemaFormValidity() {
  const [invalid, setInvalid] = useState<Set<string>>(() => new Set());
  const onValidityChange = useCallback<ValidityChange>(
    (path, valid) =>
      setInvalid((current) => {
        const next = new Set(current);
        if (valid) next.delete(path);
        else next.add(path);
        return next;
      }),
    [],
  );
  return { valid: invalid.size === 0, onValidityChange };
}

function resolveLocalSchema(schema: Schema, root: Schema): Schema {
  const seen = new Set<string>();
  let result = schema;
  while (
    typeof result.$ref === "string" &&
    result.$ref.startsWith("#/") &&
    !seen.has(result.$ref)
  ) {
    seen.add(result.$ref);
    let value: unknown = root;
    for (const part of result.$ref.slice(2).split("/"))
      value = record(value)?.[part.replaceAll("~1", "/").replaceAll("~0", "~")];
    const target = record(value);
    if (!target) break;
    const { $ref: _reference, ...own } = result;
    void _reference;
    result = { ...target, ...own };
  }
  return result;
}

function mergeObjectBranches(schema: Schema, root: Schema): Schema {
  if (!Array.isArray(schema.allOf)) return schema;
  const { allOf, ...base } = schema;
  let merged = base;
  for (const entry of allOf) {
    const branch = record(entry);
    if (!branch) return schema;
    const next = resolveLocalSchema(branch, root);
    if (next.type && next.type !== "object") return schema;
    const properties = { ...record(merged.properties) };
    for (const [key, child] of Object.entries(record(next.properties) ?? {})) {
      const existing = record(properties[key]);
      properties[key] = existing ? { allOf: [existing, child] } : child;
    }
    merged = {
      ...next,
      ...merged,
      type: "object",
      properties,
      required: Array.from(
        new Set([
          ...(Array.isArray(merged.required) ? merged.required : []),
          ...(Array.isArray(next.required) ? next.required : []),
        ]),
      ),
    };
  }
  return merged;
}

function matchesBranch(schema: Schema, value: unknown): boolean {
  if ("const" in schema && jsonText(schema.const) !== jsonText(value))
    return false;
  if (
    Array.isArray(schema.enum) &&
    !schema.enum.some((item) => jsonText(item) === jsonText(value))
  )
    return false;
  if (schema.type === "object") {
    const object = record(value);
    if (!object) return false;
    if (
      Array.isArray(schema.required) &&
      !schema.required.every((key) => typeof key === "string" && key in object)
    )
      return false;
    return Object.entries(record(schema.properties) ?? {}).every(
      ([key, child]) =>
        !(key in object) ||
        !record(child) ||
        matchesBranch(record(child) ?? {}, object[key]),
    );
  }
  if (schema.type === "string") return typeof value === "string";
  if (schema.type === "boolean") return typeof value === "boolean";
  if (schema.type === "number") return typeof value === "number";
  if (schema.type === "integer")
    return typeof value === "number" && Number.isInteger(value);
  if (schema.type === "array") return Array.isArray(value);
  if (schema.type === "null") return value === null;
  return true;
}

export function schemaDefaults(schema: Schema): unknown {
  if ("default" in schema) return structuredClone(schema.default);
  if (schema.type === "object") {
    const result: Record<string, unknown> = {};
    const properties = record(schema.properties) ?? {};
    for (const [key, value] of Object.entries(properties)) {
      const child = record(value);
      if (!child) continue;
      if ("default" in child) result[key] = structuredClone(child.default);
    }
    return result;
  }
  return undefined;
}

const CompactSchemaContext = createContext(false);
const FileWorkflowContext = createContext<string | undefined>(undefined);
type SchemaFormProps = {
  schema: Schema;
  value: unknown;
  onChange: (value: unknown) => void;
  path?: string;
  rootSchema?: Schema;
  onValidityChange?: ValidityChange;
  fieldOrder?: Record<string, string[]>;
  schemaPath?: string;
};

export function SchemaForm(
  props: SchemaFormProps & {
    compact?: boolean;
    fileWorkflowId?: string;
    inputPage?: PageDefinition;
  },
) {
  const inherited = useContext(CompactSchemaContext);
  const workflowId = useContext(FileWorkflowContext);
  return (
    <FileWorkflowContext.Provider value={props.fileWorkflowId ?? workflowId}>
      <CompactSchemaContext.Provider value={props.compact ?? inherited}>
        <SchemaFormPresentation {...props} />
      </CompactSchemaContext.Provider>
    </FileWorkflowContext.Provider>
  );
}

/** 字段仍复用现有控件与校验，布局树统一交给 json-render。 */
function SchemaFormPresentation(
  props: SchemaFormProps & { inputPage?: PageDefinition },
) {
  const root = props.rootSchema ?? props.schema;
  const schema = mergeObjectBranches(
    resolveLocalSchema(props.schema, root),
    root,
  );
  const properties = record(schema.properties);
  const orderPath =
    typeof props.schema.$ref === "string" && props.schema.$ref.startsWith("#/")
      ? props.schema.$ref.slice(1)
      : (props.schemaPath ?? "");
  const keys =
    schema.type === "object" &&
    !schema.oneOf &&
    !schema.anyOf &&
    properties &&
    (props.path ?? "args").split("/").length < 16
      ? Array.from(
          new Set([
            ...(props.fieldOrder?.[orderPath] ?? []).filter(
              (key) => key in properties,
            ),
            ...Object.keys(properties),
          ]),
        )
      : [];
  const spec: Spec = {
    root: "layout",
    elements: {
      layout: {
        type: "Stack",
        props: { gap: "medium" },
        children: keys.length
          ? keys.map((_, index) => `field-${index}`)
          : ["root-field"],
      },
    },
  };
  if (keys.length)
    keys.forEach((key, index) => {
      spec.elements[`field-${index}`] = {
        type: "Field",
        props: {
          path: `/args/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`,
        },
      };
    });
  else
    spec.elements["root-field"] = { type: "Field", props: { path: "/args" } };
  return (
    <InputRenderer
      spec={spec}
      page={props.inputPage}
      value={props.value}
      renderField={(pointer) => {
        if (pointer === "/args") return <SchemaFormFields {...props} />;
        const relative = pointer.slice(5);
        const segments = relative
          .slice(1)
          .split("/")
          .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"));
        let field = schema;
        let required = false;
        let fieldSchemaPath = orderPath;
        for (const key of segments) {
          required =
            Array.isArray(field.required) && field.required.includes(key);
          field = resolveLocalSchema(
            record(record(field.properties)?.[key]) ?? {},
            root,
          );
          fieldSchemaPath += `/properties/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`;
        }
        const fieldValue = segments.reduce<unknown>((value, key) => {
          const object = record(value);
          return object && Object.hasOwn(object, key) ? object[key] : undefined;
        }, props.value);
        return (
          <SchemaField
            name={segments.at(-1) ?? "args"}
            path={`${props.path ?? "args"}/${segments.join("/")}`}
            schema={field}
            rootSchema={root}
            fieldOrder={props.fieldOrder}
            schemaPath={fieldSchemaPath}
            required={required}
            provided={fieldValue !== undefined}
            value={fieldValue}
            onValidityChange={props.onValidityChange}
            onChange={(next, provided) => {
              const replace = (value: unknown, depth: number): unknown => {
                const key = segments[depth] ?? "";
                const object = record(value) ?? {};
                const entries = Object.entries(object).filter(
                  ([name]) => name !== key,
                );
                if (depth < segments.length - 1)
                  entries.push([
                    key,
                    replace(
                      Object.hasOwn(object, key) ? object[key] : undefined,
                      depth + 1,
                    ),
                  ]);
                else if (provided) entries.push([key, next]);
                return Object.fromEntries(entries);
              };
              props.onChange(replace(props.value, 0));
            }}
          />
        );
      }}
    />
  );
}

function SchemaFormFields({
  schema: originalSchema,
  value,
  onChange,
  path = "args",
  rootSchema = originalSchema,
  onValidityChange,
  fieldOrder = {},
  schemaPath = "",
}: {
  schema: Schema;
  value: unknown;
  onChange: (value: unknown) => void;
  path?: string;
  rootSchema?: Schema;
  onValidityChange?: ValidityChange;
  fieldOrder?: Record<string, string[]>;
  schemaPath?: string;
}) {
  const { t } = useTranslation("workflow");
  const schema = mergeObjectBranches(
    resolveLocalSchema(originalSchema, rootSchema),
    rootSchema,
  );
  const properties = record(schema.properties);
  const orderPath =
    typeof originalSchema.$ref === "string" &&
    originalSchema.$ref.startsWith("#/")
      ? originalSchema.$ref.slice(1)
      : schemaPath;
  const propertyKeys = properties
    ? Array.from(
        new Set([
          ...(fieldOrder[orderPath] ?? []).filter((key) => key in properties),
          ...Object.keys(properties),
        ]),
      )
    : [];
  const variants = schema.oneOf ?? schema.anyOf;
  if (
    schema.format === "file" ||
    (schema.type === "array" &&
      resolveLocalSchema(record(schema.items) ?? {}, rootSchema).format ===
        "file")
  )
    return (
      <SchemaField
        name={typeof schema.title === "string" ? schema.title : path}
        path={path}
        schema={schema}
        rootSchema={rootSchema}
        required
        provided={value !== undefined}
        value={value}
        onChange={onChange}
        onValidityChange={onValidityChange}
      />
    );
  if (
    Array.isArray(variants) &&
    variants.every((entry) => Boolean(record(entry))) &&
    path.split("/").length < 16
  )
    return (
      <SchemaVariants
        schema={schema}
        variants={variants.flatMap((entry) =>
          record(entry) ? [record(entry) ?? {}] : [],
        )}
        rootSchema={rootSchema}
        value={value}
        onChange={onChange}
        path={path}
        fieldOrder={fieldOrder}
        schemaPath={orderPath}
        onValidityChange={onValidityChange}
      />
    );
  if (
    schema.type === "object" &&
    properties &&
    !schema.oneOf &&
    !schema.anyOf &&
    path.split("/").length < 16
  ) {
    const object = record(value) ?? {};
    const required = Array.isArray(schema.required) ? schema.required : [];
    return (
      <div className="grid min-w-0 gap-4">
        {propertyKeys.map((key) => {
          const property = properties[key];
          const child = record(property);
          if (!child) return null;
          return (
            <SchemaField
              key={key}
              name={key}
              path={`${path}/${key}`}
              schema={child}
              rootSchema={rootSchema}
              fieldOrder={fieldOrder}
              schemaPath={`${orderPath}/properties/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`}
              onValidityChange={onValidityChange}
              required={required.includes(key)}
              provided={key in object}
              value={object[key]}
              onChange={(next, provided) => {
                const copy = Object.fromEntries(
                  Object.entries(object).filter(([name]) => name !== key),
                );
                if (provided) copy[key] = next;
                onChange(copy);
              }}
            />
          );
        })}
      </div>
    );
  }
  return (
    <JsonField
      label={typeof schema.title === "string" ? schema.title : path}
      help={
        typeof schema.description === "string"
          ? schema.description
          : t("jsonHelp")
      }
      value={value}
      onChange={onChange}
      onValidityChange={onValidityChange}
      path={path}
    />
  );
}

function SchemaVariants({
  schema,
  variants,
  rootSchema,
  value,
  onChange,
  path,
  fieldOrder,
  schemaPath,
  onValidityChange,
}: {
  schema: Schema;
  variants: Schema[];
  rootSchema: Schema;
  value: unknown;
  onChange: (value: unknown) => void;
  path: string;
  fieldOrder: Record<string, string[]>;
  schemaPath: string;
  onValidityChange?: ValidityChange;
}) {
  const { t } = useTranslation("workflow");
  const matches = variants.flatMap((branch, index) =>
    matchesBranch(resolveLocalSchema(branch, rootSchema), value) ? [index] : [],
  );
  const [selected, setSelected] = useState(
    matches.length === 1 ? String(matches[0]) : "json",
  );
  const branch = selected === "json" ? undefined : variants[Number(selected)];
  const { oneOf: _oneOf, anyOf: _anyOf, ...base } = schema;
  void _oneOf;
  void _anyOf;
  const combined = branch
    ? mergeObjectBranches(
        { allOf: [base, resolveLocalSchema(branch, rootSchema)] },
        rootSchema,
      )
    : undefined;
  return (
    <div className="grid gap-4">
      <Select
        value={selected}
        onValueChange={(next) => {
          setSelected(next);
          if (next !== "json") {
            const target = resolveLocalSchema(
              variants[Number(next)] ?? {},
              rootSchema,
            );
            const defaults = schemaDefaults(target);
            const object = record(value);
            const properties = {
              ...record(base.properties),
              ...record(target.properties),
            };
            onChange(
              object
                ? {
                    ...record(defaults),
                    ...Object.fromEntries(
                      Object.entries(object).filter(
                        ([key]) => key in properties,
                      ),
                    ),
                  }
                : (defaults ?? value),
            );
          }
        }}
      >
        <SelectTrigger aria-label={t("schemaVariant")}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="json">{t("json")}</SelectItem>
          {variants.map((entry, index) => (
            <SelectItem key={index} value={String(index)}>
              {typeof entry.title === "string"
                ? entry.title
                : `${t("schemaVariant")} ${index + 1}`}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {combined ? (
        <SchemaForm
          schema={combined}
          rootSchema={rootSchema}
          fieldOrder={fieldOrder}
          schemaPath={`${schemaPath}/${schema.oneOf ? "oneOf" : "anyOf"}/${selected}`}
          value={value}
          onChange={onChange}
          path={path}
          onValidityChange={onValidityChange}
        />
      ) : (
        <JsonField
          label={typeof schema.title === "string" ? schema.title : path}
          help={t("jsonHelp")}
          value={value}
          onChange={onChange}
          path={path}
          onValidityChange={onValidityChange}
        />
      )}
    </div>
  );
}

function SchemaField({
  name,
  path,
  schema: originalSchema,
  required,
  provided,
  value,
  onChange,
  rootSchema,
  onValidityChange,
  fieldOrder = {},
  schemaPath = "",
}: {
  name: string;
  path: string;
  schema: Schema;
  required: boolean;
  provided: boolean;
  value: unknown;
  onChange: (value: unknown, provided: boolean) => void;
  rootSchema: Schema;
  onValidityChange?: ValidityChange;
  fieldOrder?: Record<string, string[]>;
  schemaPath?: string;
}) {
  const schema = resolveLocalSchema(originalSchema, rootSchema);
  const { t } = useTranslation("workflow");
  const orderPath =
    typeof originalSchema.$ref === "string" &&
    originalSchema.$ref.startsWith("#/")
      ? originalSchema.$ref.slice(1)
      : schemaPath;
  const id = useId();
  const compact = useContext(CompactSchemaContext);
  const workflowId = useContext(FileWorkflowContext);
  const missing =
    required &&
    (!provided ||
      value === undefined ||
      (schema.format === "file" && value === ""));
  useEffect(() => {
    onValidityChange?.(`${path}/$presence`, !missing);
  }, [missing, onValidityChange, path]);
  useEffect(
    () => () => onValidityChange?.(`${path}/$presence`, true),
    [onValidityChange, path],
  );
  const label = typeof schema.title === "string" ? schema.title : name;
  const nullable = Array.isArray(schema.type) && schema.type.includes("null");
  const type = Array.isArray(schema.type)
    ? schema.type.find((item) => item !== "null")
    : schema.type;
  const help =
    typeof schema.description === "string"
      ? schema.description
      : t("fieldHelp");
  const showHelp =
    Boolean(help) &&
    help !== label &&
    (!compact || Boolean(schema.description));
  const usingDefault =
    provided &&
    "default" in schema &&
    jsonText(value) === jsonText(schema.default);
  const example = Array.isArray(schema.examples)
    ? schema.examples.find(
        (item) => typeof item === "string" || typeof item === "number",
      )
    : undefined;
  const placeholder =
    typeof example === "string" || typeof example === "number"
      ? t("fieldExample", { example: String(example) })
      : t("fieldPlaceholder", { name: label });
  const mode = !provided ? "omitted" : value === null ? "null" : "value";
  const choice = Array.isArray(schema.enum) ? schema.enum : undefined;
  const control = schema["x-ui-control"];
  const itemSchema = resolveLocalSchema(record(schema.items) ?? {}, rootSchema);
  const multipleChoices =
    type === "array" && Array.isArray(itemSchema.enum)
      ? itemSchema.enum
      : undefined;
  const choiceLabel = (item: unknown, index: number) => {
    const labels = schema["x-ui-labels"];
    return Array.isArray(labels) && typeof labels[index] === "string"
      ? labels[index]
      : typeof item === "string"
        ? item
        : jsonText(item);
  };
  const error = (() => {
    if (!provided || value === null || choice) return undefined;
    if (type === "string" && typeof value === "string") {
      const length = Array.from(value).length;
      if (typeof schema.minLength === "number" && length < schema.minLength)
        return t("fieldMinLength", { count: schema.minLength });
      if (typeof schema.maxLength === "number" && length > schema.maxLength)
        return t("fieldMaxLength", { count: schema.maxLength });
      if (typeof schema.pattern === "string") {
        try {
          if (!new RegExp(schema.pattern, "u").test(value))
            return t("fieldInvalidFormat");
        } catch {
          return t("fieldInvalidFormat");
        }
      }
    }
    if (
      (type === "number" || type === "integer") &&
      typeof value === "number"
    ) {
      if (
        !Number.isFinite(value) ||
        (type === "integer" && !Number.isInteger(value))
      )
        return t("fieldInvalidNumber");
      if (typeof schema.minimum === "number" && value < schema.minimum)
        return t("fieldMinimum", { value: schema.minimum });
      if (typeof schema.maximum === "number" && value > schema.maximum)
        return t("fieldMaximum", { value: schema.maximum });
    }
    return undefined;
  })();
  useEffect(() => {
    onValidityChange?.(`${path}/$constraint`, !error);
  }, [error, onValidityChange, path]);
  useEffect(
    () => () => onValidityChange?.(`${path}/$constraint`, true),
    [onValidityChange, path],
  );
  const description =
    [showHelp ? `${id}-help` : undefined, error ? `${id}-error` : undefined]
      .filter(Boolean)
      .join(" ") || undefined;
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <FieldLabel htmlFor={id} required={required}>
          {label}
        </FieldLabel>
        {usingDefault ? (
          <span className="text-content-caption type-caption">
            {t("fieldDefault")}
          </span>
        ) : null}
        {(!compact && (!required || missing)) || nullable ? (
          <Select
            value={mode}
            onValueChange={(next) => {
              if (next === "omitted") onChange(undefined, false);
              else if (next === "null") onChange(null, true);
              else
                onChange(
                  schemaDefaults(schema) ??
                    (type === "boolean"
                      ? false
                      : type === "object"
                        ? {}
                        : type === "array"
                          ? []
                          : type === "number" || type === "integer"
                            ? 0
                            : ""),
                  true,
                );
            }}
          >
            <SelectTrigger
              size="sm"
              aria-label={missing ? t("fieldValueMode") : label}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {!required ? (
                <SelectItem value="omitted">{t("omitted")}</SelectItem>
              ) : null}
              {nullable ? (
                <SelectItem value="null">{t("explicitNull")}</SelectItem>
              ) : null}
              <SelectItem value="value">
                {t("fieldPlaceholder", { name: label })}
              </SelectItem>
            </SelectContent>
          </Select>
        ) : null}
      </div>
      {showHelp ? (
        <p id={`${id}-help`} className="text-muted-foreground type-caption">
          {help}
        </p>
      ) : null}
      {mode === "null" ? (
        <code className="type-body">null</code>
      ) : schema.format === "file" ? (
        <SchemaFileField
          id={id}
          workflowId={workflowId}
          value={value}
          path={path}
          description={description}
          onChange={(next) => onChange(next, true)}
          onValidityChange={onValidityChange}
        />
      ) : !provided && !required && !compact ? null : choice &&
        control === "radio" ? (
        <RadioGroup
          id={id}
          aria-label={label}
          aria-describedby={showHelp ? `${id}-help` : undefined}
          value={
            provided
              ? String(
                  choice.findIndex(
                    (item) => jsonText(item) === jsonText(value),
                  ),
                )
              : ""
          }
          onValueChange={(next) => onChange(choice[Number(next)], true)}
        >
          {choice.map((item, index) => (
            <label
              key={jsonText(item)}
              className="type-body flex items-center gap-2"
            >
              <RadioGroupItem value={String(index)} />
              {choiceLabel(item, index)}
            </label>
          ))}
        </RadioGroup>
      ) : choice ? (
        <Select
          value={
            provided
              ? String(
                  choice.findIndex(
                    (item) => jsonText(item) === jsonText(value),
                  ),
                )
              : undefined
          }
          onValueChange={(next) => onChange(choice[Number(next)], true)}
        >
          <SelectTrigger
            id={id}
            aria-describedby={showHelp ? `${id}-help` : undefined}
          >
            <SelectValue placeholder={t("fieldPlaceholder", { name: label })} />
          </SelectTrigger>
          <SelectContent>
            {choice.map((item) => (
              <SelectItem
                key={jsonText(item)}
                value={String(choice.indexOf(item))}
              >
                {choiceLabel(item, choice.indexOf(item))}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : type === "boolean" ? (
        <div className="flex items-center gap-2">
          {control === "switch" ? (
            <Switch
              id={id}
              aria-label={label}
              checked={value === true}
              onClick={() => onChange(value !== true, true)}
            />
          ) : (
            <Checkbox
              id={id}
              checked={value === true}
              onCheckedChange={(checked) => onChange(checked === true, true)}
            />
          )}
          <label htmlFor={id} className="type-body">
            {value === true ? t("true") : t("false")}
          </label>
        </div>
      ) : type === "object" ? (
        <SchemaForm
          schema={{ ...schema, type: "object" }}
          rootSchema={rootSchema}
          fieldOrder={fieldOrder}
          schemaPath={orderPath}
          onValidityChange={onValidityChange}
          path={path}
          value={value}
          onChange={(next) => onChange(next, true)}
        />
      ) : multipleChoices ? (
        <div role="group" aria-label={label} className="grid gap-2">
          {multipleChoices.map((item, index) => (
            <label
              key={jsonText(item)}
              className="type-body flex items-center gap-2"
            >
              <Checkbox
                checked={
                  Array.isArray(value) &&
                  value.some((entry) => jsonText(entry) === jsonText(item))
                }
                onCheckedChange={(checked) => {
                  const current = Array.isArray(value) ? value : [];
                  onChange(
                    checked
                      ? [...current, item]
                      : current.filter(
                          (entry) => jsonText(entry) !== jsonText(item),
                        ),
                    true,
                  );
                }}
              />
              {choiceLabel(item, index)}
            </label>
          ))}
        </div>
      ) : type === "array" ? (
        <ArrayField
          schema={schema}
          rootSchema={rootSchema}
          fieldOrder={fieldOrder}
          schemaPath={orderPath}
          onValidityChange={onValidityChange}
          path={path}
          value={Array.isArray(value) ? value : []}
          onChange={(next) => onChange(next, true)}
        />
      ) : type === "string" &&
        (control === "textarea" ||
          (control !== "input" && compact && required)) ? (
        <Textarea
          id={id}
          aria-required={required}
          placeholder={placeholder}
          aria-describedby={description}
          aria-invalid={Boolean(error)}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(event.target.value, true)}
        />
      ) : type === "string" || type === "number" || type === "integer" ? (
        <Input
          id={id}
          aria-required={required}
          aria-describedby={description}
          aria-invalid={Boolean(error)}
          type={type === "string" ? "text" : "number"}
          step="any"
          placeholder={placeholder}
          value={
            typeof value === "string" || typeof value === "number" ? value : ""
          }
          onChange={(event) =>
            onChange(
              type === "string"
                ? event.target.value
                : event.target.value === ""
                  ? undefined
                  : Number(event.target.value),
              event.target.value !== "" || type === "string",
            )
          }
        />
      ) : (
        <JsonField
          label={label}
          help={help}
          value={value}
          onChange={(next) => onChange(next, true)}
          onValidityChange={onValidityChange}
          path={path}
        />
      )}
      {error ? (
        <p
          id={`${id}-error`}
          role="alert"
          className="text-destructive type-body"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}

function ArrayField({
  schema,
  path,
  value,
  onChange,
  rootSchema,
  onValidityChange,
  fieldOrder = {},
  schemaPath = "",
}: {
  schema: Schema;
  path: string;
  value: unknown[];
  onChange: (value: unknown[]) => void;
  rootSchema: Schema;
  onValidityChange?: ValidityChange;
  fieldOrder?: Record<string, string[]>;
  schemaPath?: string;
}) {
  const { t } = useTranslation("workflow");
  const [keys, setKeys] = useState(() => value.map(() => crypto.randomUUID()));
  if (keys.length !== value.length)
    setKeys((current) =>
      value.map((_, index) => current[index] ?? crypto.randomUUID()),
    );
  const item = record(schema.items) ?? {};
  return (
    <div className="grid gap-3">
      {value.map((entry, index) => (
        <div key={keys[index]} className="bg-muted grid gap-2 rounded-lg p-3">
          <SchemaField
            name={String(index + 1)}
            path={`${path}/${index}`}
            schema={item}
            rootSchema={rootSchema}
            fieldOrder={fieldOrder}
            schemaPath={`${schemaPath}/items`}
            onValidityChange={onValidityChange}
            required
            provided
            value={entry}
            onChange={(next) =>
              onChange(
                value.map((old, itemIndex) =>
                  itemIndex === index ? next : old,
                ),
              )
            }
          />
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setKeys(keys.filter((_, itemIndex) => itemIndex !== index));
              onChange(value.filter((_, itemIndex) => itemIndex !== index));
            }}
          >
            {t("removeItem")}
          </Button>
        </div>
      ))}
      <Button
        size="sm"
        variant="secondary"
        onClick={() => {
          setKeys([...keys, crypto.randomUUID()]);
          onChange([
            ...value,
            schemaDefaults(item) ?? (item.type === "object" ? {} : ""),
          ]);
        }}
      >
        {t("addItem")}
      </Button>
    </div>
  );
}

function JsonField({
  label,
  help,
  value,
  onChange,
  path,
  onValidityChange,
}: {
  label: string;
  help: string;
  value: unknown;
  onChange: (value: unknown) => void;
  path: string;
  onValidityChange?: ValidityChange;
}) {
  const { t } = useTranslation("workflow");
  const id = useId();
  const signature = jsonText(value);
  const [draft, setDraft] = useState(() => ({
    text: signature,
    signature,
    invalid: false,
  }));
  if (draft.signature !== signature)
    setDraft({ text: signature, signature, invalid: false });
  const { text, invalid } = draft;
  useEffect(() => {
    onValidityChange?.(path, !invalid);
  }, [onValidityChange, path, invalid]);
  useEffect(
    () => () => onValidityChange?.(path, true),
    [onValidityChange, path],
  );
  return (
    <div className="grid gap-2">
      <label htmlFor={id} className="type-body font-medium">
        {label}
      </label>
      <p className="text-muted-foreground type-caption">{help}</p>
      <Textarea
        id={id}
        placeholder={t("jsonPlaceholder")}
        value={text}
        onChange={(event) => {
          try {
            const parsed: unknown = JSON.parse(event.target.value);
            setDraft({
              text: event.target.value,
              signature: jsonText(parsed),
              invalid: false,
            });
            onChange(parsed);
            onValidityChange?.(path, true);
          } catch {
            setDraft({ text: event.target.value, signature, invalid: true });
            onValidityChange?.(path, false);
          }
        }}
      />
      {invalid ? (
        <p className="text-destructive type-body" role="alert">
          {t("invalidJson")}
        </p>
      ) : null}
    </div>
  );
}
