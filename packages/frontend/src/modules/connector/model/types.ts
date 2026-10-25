import type {
  GithubComJieWaZiNeuxBotServerApisMcpConnectorResource,
  GithubComJieWaZiNeuxBotServerApisMcpConnectorResponse,
  GithubComJieWaZiNeuxBotServerApisMcpRuntimeCompatibility,
  GithubComJieWaZiNeuxBotServerApisMcpSecretInput,
} from "@/shared/api/generated/types.gen";

export type ConnectorTransport = "stdio" | "streamable_http" | "sse";
export type ConnectorScope = "built_in" | "public" | "private";
export type ConnectorTestStatus = "untested" | "succeeded" | "failed";
export type ConnectorActivationPolicy =
  "per_execution" | "team_context" | "workflow_context";

export type ConnectorView =
  GithubComJieWaZiNeuxBotServerApisMcpConnectorResponse;
export type ConnectorRecord =
  GithubComJieWaZiNeuxBotServerApisMcpConnectorResource;
export type ConnectorCompatibility =
  GithubComJieWaZiNeuxBotServerApisMcpRuntimeCompatibility;

export type SecretField = GithubComJieWaZiNeuxBotServerApisMcpSecretInput & {
  configured?: boolean;
};

export type ConnectorFormValue = {
  argumentsText: string;
  command: string;
  description: string;
  environment: SecretField[];
  headers: SecretField[];
  name: string;
  timeoutMS: string;
  transport: ConnectorTransport;
  url: string;
};

export type ConnectorSummary = {
  activationPolicy: ConnectorActivationPolicy;
  affectedWakerCount: number;
  compatibility: ConnectorCompatibility[];
  description: string;
  enabled: boolean;
  id: string;
  name: string;
  scope: ConnectorScope;
  testStatus: ConnectorTestStatus;
  toolCount: number;
  transport: ConnectorTransport;
};
