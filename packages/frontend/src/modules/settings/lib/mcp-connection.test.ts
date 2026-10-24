import { describe, expect, it } from "vitest";
import { mcpClientConfig, mcpConnection } from "./mcp-connection";

describe("MCP 已保存地址与配置", () => {
  it.each([
    ["https://example.test/api/v1", "https://example.test/mcp", false],
    [
      " https://example.test/proxy/api/v1/ ",
      "https://example.test/proxy/mcp",
      false,
    ],
    [
      "https://example.test/proxy/v1",
      "https://example.test/proxy/v1/mcp",
      false,
    ],
    [
      "https://example.test/a%20b/api/v1",
      "https://example.test/a%20b/mcp",
      false,
    ],
    [
      "http://[2001:db8::1]:8431/proxy/api/v1",
      "http://[2001:db8::1]:8431/proxy/mcp",
      false,
    ],
    ["http://127.example.test/api/v1", "http://127.example.test/mcp", false],
    ["http://127.0.0.1:8431/api/v1", "http://127.0.0.1:8431/mcp", true],
    ["http://127.12.0.2:8431/api/v1", "http://127.12.0.2:8431/mcp", true],
    ["http://[::1]:8431/api/v1", "http://[::1]:8431/mcp", true],
    [
      "http://[::ffff:127.0.0.1]:8431/api/v1",
      "http://[::ffff:7f00:1]:8431/mcp",
      true,
    ],
    ["http://localhost:8431/api/v1", "http://localhost:8431/mcp", true],
    ["http://dev.localhost:8431/api/v1", "http://dev.localhost:8431/mcp", true],
  ])("从 %s 派生地址并保留代理前缀", (base, url, loopback) => {
    expect(mcpConnection(base)).toEqual({ url, loopback });
  });

  it.each([
    undefined,
    "",
    "  ",
    "/api/v1",
    "not a url",
    "ftp://example.test/api/v1",
    "http://0.0.0.0:8431/api/v1",
    "http://[::]:8431/api/v1",
    "https://user:secret@example.test/api/v1",
    "https://example.test/api/v1?key=secret",
    "https://example.test/api/v1#hash",
  ])("不为未确定或无效地址 %s 生成配置", (base) => {
    expect(mcpConnection(base)).toBeUndefined();
  });

  it("只生成通用 HTTP 配置与 API Key 占位符", () => {
    expect(
      JSON.parse(mcpClientConfig("https://example.test/proxy/mcp")),
    ).toEqual({
      mcpServers: {
        neuxally: {
          url: "https://example.test/proxy/mcp",
          headers: { "X-API-Key": "<YOUR_API_KEY>" },
        },
      },
    });
  });
});
