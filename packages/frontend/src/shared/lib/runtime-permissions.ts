export type PermissionLevel = "default" | "auto" | "full-access";

export const permissionLevels: readonly PermissionLevel[] = [
  "default",
  "auto",
  "full-access",
];

export const permissionLevelTranslationKeys = {
  default: {
    label: "permission.level.default.label",
    description: "permission.level.default.description",
  },
  auto: {
    label: "permission.level.auto.label",
    description: "permission.level.auto.description",
  },
  "full-access": {
    label: "permission.level.fullAccess.label",
    description: "permission.level.fullAccess.description",
  },
} as const;

export const permissionLevelToneClasses = {
  default: {
    icon: "text-success",
    title: "text-success",
    trigger: "text-success hover:text-success",
  },
  auto: {
    icon: "text-warning",
    title: "text-warning",
    trigger: "text-warning hover:text-warning",
  },
  "full-access": {
    icon: "text-destructive",
    title: "text-destructive",
    trigger: "text-destructive hover:text-destructive",
  },
} satisfies Record<
  PermissionLevel,
  { icon: string; title: string; trigger: string }
>;

type RuntimePermissionSource = {
  defaultModel?: string;
  models?: ReadonlyArray<{
    id?: string;
    permissionLevels?: ReadonlyArray<string>;
  }>;
};

export function permissionLevelOf(value?: string): PermissionLevel | undefined {
  return permissionLevels.find((level) => level === value);
}

export function supportedPermissionLevels(
  values?: readonly string[],
): ReadonlySet<PermissionLevel> {
  return new Set(values?.flatMap((value) => permissionLevelOf(value) ?? []));
}

export function modelPermissionLevels(
  models: RuntimePermissionSource["models"],
  modelID?: string,
): ReadonlySet<PermissionLevel> {
  const selectedID = modelID?.trim() || models?.[0]?.id || "";
  const selected = models?.find((model) => model.id === selectedID);
  return supportedPermissionLevels(selected?.permissionLevels);
}

export function runtimeDefaultPermissionLevels(
  runtime: RuntimePermissionSource,
): ReadonlySet<PermissionLevel> {
  return modelPermissionLevels(runtime.models, runtime.defaultModel);
}

export function safeDefaultPermissionLevel(
  preferred: PermissionLevel,
  supported: ReadonlySet<PermissionLevel>,
): PermissionLevel {
  if (supported.has(preferred)) return preferred;
  if (supported.size === 0) return preferred;
  if (supported.has("auto")) return "auto";
  if (supported.has("default")) return "default";
  const [first] = supported;
  return first ?? preferred;
}
