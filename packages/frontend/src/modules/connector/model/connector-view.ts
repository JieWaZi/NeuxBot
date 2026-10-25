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
  argumentsText: "",
  command: "",
  description: "",
  environment: [],
  headers: [],
  name: "",
  timeoutMS: "10000",
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

export function toConnectorFormValue(
  connector?: ConnectorRecord,
): ConnectorFormValue {
  if (!connector) return { ...defaultConnectorFormValue };
  return {
    argumentsText: formatCommandLineArguments(connector.arguments ?? []),
    command: connector.command ?? "",
    description: connector.description ?? "",
    environment: toSecretFields(connector.environment),
    headers: toSecretFields(connector.headers),
    name: connector.name ?? "",
    timeoutMS: String(connector.timeoutMs ?? 10000),
    transport: connectorTransportOf(connector.transport),
    url: connector.url ?? "",
  };
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
