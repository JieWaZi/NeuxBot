/** MCP 只派生已保存的发布地址，不从监听 IP 或未保存表单猜测访问主机。 */
export function mcpConnection(publicBaseURL: string | undefined) {
  if (!publicBaseURL?.trim()) return undefined;
  let address: URL;
  try {
    address = new URL(publicBaseURL.trim());
  } catch {
    return undefined;
  }
  if (
    !["http:", "https:"].includes(address.protocol) ||
    address.username ||
    address.password ||
    address.search ||
    address.hash ||
    ["0.0.0.0", "[::]"].includes(address.hostname)
  )
    return undefined;

  const prefix = address.pathname.replace(/\/+$/, "").replace(/\/api\/v1$/, "");
  address.pathname = `${prefix}/mcp`;
  const hostname = address.hostname.toLowerCase();
  return {
    url: address.toString(),
    loopback:
      hostname === "localhost" ||
      hostname.endsWith(".localhost") ||
      /^127\.\d+\.\d+\.\d+$/.test(hostname) ||
      hostname === "[::1]" ||
      /^\[::ffff:7f[0-9a-f]{2}:[0-9a-f]+\]$/.test(hostname),
  };
}

/** 配置只含替换占位符；此接入视图不接收、读取或保存密钥明文。 */
export function mcpClientConfig(url: string) {
  return JSON.stringify(
    {
      mcpServers: {
        neuxally: {
          url,
          headers: { "X-API-Key": "<YOUR_API_KEY>" },
        },
      },
    },
    null,
    2,
  );
}
