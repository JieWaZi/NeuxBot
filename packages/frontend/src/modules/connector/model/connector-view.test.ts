import { describe, expect, it } from "vitest";

import {
  connectorSelectableInComposer,
  connectorTimeoutMsFromSeconds,
  connectorTrustFieldsChanged,
  parseArguments,
  toConnectorFormValue,
  toConnectorSummary,
} from "./connector-view";

describe("connector view model", () => {
  it.each([
    [100, "0.1"],
    [1000, "1"],
    [4001, "4.001"],
    [12345, "12.345"],
    [300000, "300"],
  ])("往返保留 %i 毫秒的秒值 %s", (milliseconds, seconds) => {
    const form = toConnectorFormValue({ timeoutMs: milliseconds });
    expect(form.timeoutSeconds).toBe(seconds);
    expect(connectorTimeoutMsFromSeconds(form.timeoutSeconds)).toBe(
      milliseconds,
    );
  });

  it("新建默认十秒并在提交边界恢复整数毫秒", () => {
    const form = toConnectorFormValue();
    expect(form.timeoutSeconds).toBe("10");
    expect(connectorTimeoutMsFromSeconds(form.timeoutSeconds)).toBe(10000);
  });

  it.each(["", " ", "invalid", "Infinity", "0.099", "300.001", "0.1001"])(
    "拒绝空值、超范围及不足一毫秒精度的输入 %s",
    (value) => {
      expect(connectorTimeoutMsFromSeconds(value)).toBeNull();
    },
  );

  it("converts an API view into a stable card summary", () => {
    expect(
      toConnectorSummary({
        affectedWakerCount: 3,
        enabled: true,
        connector: {
          activationPolicy: "team_context",
          id: "connector-1",
          name: "文件系统",
          scope: "built_in",
          testStatus: "succeeded",
          testToolCount: 4,
          transport: "stdio",
        },
      }),
    ).toMatchObject({
      activationPolicy: "team_context",
      affectedWakerCount: 3,
      enabled: true,
      id: "connector-1",
      name: "文件系统",
      scope: "built_in",
      testStatus: "succeeded",
      toolCount: 4,
      transport: "stdio",
    });
  });

  it("旧连接器缺少策略时按每轮执行挂载处理", () => {
    expect(
      toConnectorSummary({
        enabled: true,
        connector: {
          id: "connector-1",
          name: "文件系统",
          transport: "stdio",
        },
      }),
    ).toMatchObject({ activationPolicy: "per_execution" });
  });

  it("只把按执行挂载的连接器暴露给聊天输入框", () => {
    expect(connectorSelectableInComposer("per_execution")).toBe(true);
    expect(connectorSelectableInComposer("team_context")).toBe(false);
    expect(connectorSelectableInComposer("workflow_context")).toBe(false);
    expect(connectorSelectableInComposer(undefined)).toBe(true);
  });

  it("保留工作流的系统启用策略", () => {
    expect(
      toConnectorSummary({
        enabled: true,
        connector: {
          id: "workflow",
          name: "内置工作流",
          activationPolicy: "workflow_context",
          transport: "streamable_http",
        },
      }),
    ).toMatchObject({ activationPolicy: "workflow_context", enabled: true });
  });

  it("keeps configured secret metadata without exposing a value", () => {
    const form = toConnectorFormValue({
      environment: [{ configured: true, id: "token", name: "API_TOKEN" }],
      headers: [
        {
          configured: true,
          id: "authorization",
          name: "Authorization",
        },
      ],
    });

    expect(form.environment).toEqual([
      {
        configured: true,
        id: "token",
        name: "API_TOKEN",
        value: "",
      },
    ]);
    expect(form.headers).toEqual([
      {
        configured: true,
        id: "authorization",
        name: "Authorization",
        value: "",
      },
    ]);
  });

  it("requires new trust only when execution-relevant fields change", () => {
    const initial = toConnectorFormValue({
      command: "npx",
      arguments: ["-y", "server"],
      environment: [{ id: "token", name: "API_TOKEN" }],
      name: "原名称",
      transport: "stdio",
    });

    expect(
      connectorTrustFieldsChanged(initial, {
        ...initial,
        name: "新名称",
        timeoutSeconds: "20",
      }),
    ).toBe(false);
    expect(
      connectorTrustFieldsChanged(initial, {
        ...initial,
        command: "bunx",
      }),
    ).toBe(true);
  });

  it("parses pasted STDIO arguments using command-line syntax", () => {
    expect(parseArguments(' -y server --name "Chrome MCP" ')).toEqual([
      "-y",
      "server",
      "--name",
      "Chrome MCP",
    ]);
  });
});
