import type {
  ConnectorActivationPolicy,
  ConnectorFormValue,
  ConnectorRecord,
  ConnectorSummary,
  ConnectorTransport,
  ConnectorView,
  SecretField,
} from "./types";
import {
  formatCommandLineArguments,
  parseCommandLineArguments,
} from "@/shared/lib/command-line-arguments";

export const defaultConnectorFormValue: ConnectorFormValue = {
  categories: [],
  argumentsText: "",
  command: "",
  description: "",
  environment: [],
  headers: [],
  name: "",
  timeoutSeconds: "10",
  transport: "stdio",
  url: "",
};

export function toConnectorSummary(
  view: ConnectorView,
): ConnectorSummary | null {
  const connector = view.connector;
  if (!connector?.id || !connector.name) return null;
  const transport = connectorTransportOf(connector.transport);
  const scope =
    connector.scope === "built_in"
      ? "built_in"
      : connector.scope === "private"
        ? "private"
        : "public";
  const testStatus =
    connector.testStatus === "succeeded" || connector.testStatus === "failed"
      ? connector.testStatus
      : "untested";
  return {
    activationPolicy: connectorActivationPolicyOf(connector.activationPolicy),
    affectedWakerCount: view.affectedWakerCount ?? 0,
    compatibility: view.compatibility ?? [],
    description: connector.description ?? "",
    enabled: view.enabled ?? false,
    id: connector.id,
    name: connector.name,
    scope,
    testStatus,
    toolCount: connector.testToolCount ?? 0,
    transport,
  };
}

export function connectorActivationPolicyOf(
  value: unknown,
): ConnectorActivationPolicy {
  return value === "team_context" || value === "workflow_context"
    ? value
    : "per_execution";
}

export function connectorSelectableInComposer(
  activationPolicy: unknown,
): boolean {
  return connectorActivationPolicyOf(activationPolicy) === "per_execution";
}

/** 试用引用只来自所选伙伴当前已启用且可进入普通聊天的安装记录。 */
export function connectorReferenceAvailable(
  view: ConnectorView | undefined,
  connectorId: string,
): boolean {
  return Boolean(
    connectorId &&
    view?.connector?.id === connectorId &&
    view.enabled === true &&
    connectorSelectableInComposer(view.connector.activationPolicy),
  );
}

export function toConnectorFormValue(
  connector?: ConnectorRecord,
): ConnectorFormValue {
  if (!connector) return { ...defaultConnectorFormValue };
  return {
    argumentsText: formatCommandLineArguments(connector.arguments ?? []),
    categories: connector.categories ?? [],
    command: connector.command ?? "",
    description: connector.description ?? "",
    environment: toSecretFields(connector.environment),
    headers: toSecretFields(connector.headers),
    name: connector.name ?? "",
    timeoutSeconds: String((connector.timeoutMs ?? 10000) / 1000),
    transport: connectorTransportOf(connector.transport),
    url: connector.url ?? "",
  };
}

/** 编辑使用秒；提交时恢复整数毫秒，不丢弃历史值的毫秒精度。 */
export function connectorTimeoutMsFromSeconds(value: string): number | null {
  const seconds = Number(value);
  if (
    !value.trim() ||
    !Number.isFinite(seconds) ||
    seconds < 0.1 ||
    seconds > 300
  )
    return null;
  // 先消除乘法的二进制尾差，再用往返比较拒绝不足一毫秒的输入。
  const milliseconds = Math.round(seconds * 1000);
  return milliseconds / 1000 === seconds ? milliseconds : null;
}

export function connectorTrustFieldsChanged(
  initial: ConnectorFormValue | undefined,
  current: ConnectorFormValue,
): boolean {
  if (!initial) return true;
  return (
    JSON.stringify(trustProjection(initial)) !==
    JSON.stringify(trustProjection(current))
  );
}

export function connectorTransportOf(value: unknown): ConnectorTransport {
  return value === "streamable_http" || value === "sse" ? value : "stdio";
}

function toSecretFields(
  definitions: ConnectorRecord["environment"],
): SecretField[] {
  return (definitions ?? []).map((definition) => ({
    configured: definition.configured ?? false,
    id: definition.id,
    name: definition.name ?? "",
    value: "",
  }));
}

function trustProjection(value: ConnectorFormValue) {
  return {
    arguments: trustArguments(value.argumentsText),
    command: value.command.trim(),
    environmentNames: value.environment.map((item) => item.name?.trim() ?? ""),
    headerNames: value.headers.map((item) => item.name?.trim() ?? ""),
    transport: value.transport,
    url: value.url.trim(),
  };
}

export function parseArguments(value: string): string[] {
  return parseCommandLineArguments(value);
}

function trustArguments(value: string) {
  try {
    return parseArguments(value);
  } catch {
    return [value];
  }
}
