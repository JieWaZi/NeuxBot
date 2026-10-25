import { afterEach, describe, expect, it, vi } from "vitest";
import { requestSession, SessionError } from "./session";

afterEach(() => vi.unstubAllGlobals());
describe("本机 Web 会话", () => {
  it("无会话时自动建立本机会话，Cookie 使用同源策略", async () => {
    const session = {
      protocolVersion: 1,
      csrfToken: "csrf",
      expiresAt: 123,
      capabilities: { workspace: false, git: false, terminal: false },
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(
        new Response(JSON.stringify(session), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetch);
    expect(await requestSession()).toEqual(session);
    expect(fetch.mock.calls[0]?.[1].method).toBeUndefined();
    const call = fetch.mock.calls[1];
    if (!call) throw new Error("缺少会话建立请求");
    const [url, init] = call;
    expect(url).toBe("/api/web/session");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect(init.body).toBeUndefined();
    expect(init.headers).toBeUndefined();
  });
  it("刷新复用 Cookie，401 保持认证失效语义", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 401 }));
    vi.stubGlobal("fetch", fetch);
    await expect(requestSession()).rejects.toEqual(new SessionError(401));
    expect(fetch.mock.calls[0]?.[1].method).toBeUndefined();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[0]?.[1].body).toBeUndefined();
  });
  it("刷新复用已有会话，不重复建立会话", async () => {
    const session = {
      protocolVersion: 1,
      csrfToken: "csrf",
      expiresAt: 123,
      capabilities: { workspace: true, git: true, terminal: true },
    };
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify(session)));
    vi.stubGlobal("fetch", fetch);
    expect(await requestSession()).toEqual(session);
    expect(fetch).toHaveBeenCalledOnce();
  });
  it("拒绝访问时不尝试建立会话", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 403 }));
    vi.stubGlobal("fetch", fetch);
    await expect(requestSession()).rejects.toEqual(new SessionError(403));
    expect(fetch).toHaveBeenCalledOnce();
  });
  it("拒绝不匹配的网页协议", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            protocolVersion: 2,
            csrfToken: "csrf",
            expiresAt: 123,
            capabilities: {},
          }),
        ),
      ),
    );
    await expect(requestSession()).rejects.toEqual(new SessionError(409));
  });
});
