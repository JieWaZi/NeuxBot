import { act, render, renderHook } from "@testing-library/react";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import { initializeApi } from "@/shared/api/runtime";
import { ApiError } from "@/shared/api/errors";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";

vi.mock("@/shared/platform", () => ({
  getCoreConnection: async () => ({
    baseUrl: "http://127.0.0.1:19876",
    token: "test",
  }),
}));
beforeAll(async () => {
  await initializeApi();
});
afterEach(() => {
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});

it.each(["ReadFailed", "NETWORK"])(
  "本地消费的%s保留请求身份，卸载后的同请求成功恢复原通知",
  async (code) => {
    const notices = renderHook(useErrorNotices);
    client.setConfig({
      fetch: async () => {
        if (code === "NETWORK") throw new TypeError("Failed to fetch");
        return new Response(
          JSON.stringify({ key: code, message: "读取失败" }),
          { status: 503, headers: { "Content-Type": "application/json" } },
        );
      },
    });
    const options = withLocalErrorSurface({
      url: "/api/v1/knowledge/libraries/kb/pages",
      throwOnError: true as const,
    });
    const error = await client.get(options).then(
      () => {
        throw new Error("预期读取失败");
      },
      (failure: unknown) => failure,
    );
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      requestOperation: "api:GET:/api/v1/knowledge/libraries/kb/pages",
    });
    expect(notices.result.current).toHaveLength(0);
    const old = render(
      <KnowledgeReadFeedback
        state={{ pending: false, error, retry: vi.fn() }}
      />,
    );
    expect(notices.result.current).toHaveLength(1);
    old.unmount();
    client.setConfig({
      fetch: async () =>
        new Response("{}", { headers: { "Content-Type": "application/json" } }),
    });
    await act(async () => {
      await client.get(options);
    });
    render(
      <KnowledgeReadFeedback
        state={{ pending: false, error: undefined, retry: vi.fn() }}
      />,
    );
    expect(notices.result.current).toHaveLength(0);
  },
);
