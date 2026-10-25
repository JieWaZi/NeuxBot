import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it } from "vitest";
import "@/shared/i18n";
import { useKnowledgeWrites } from "./knowledge-writes";
import { ApiError } from "@/shared/api/errors";

function setup() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(
    ({ id }) =>
      useKnowledgeWrites({
        backend: "local",
        connectionId: "local",
        knowledgeBaseId: id,
      }),
    { initialProps: { id: "a" }, wrapper },
  );
}
it("整理错误只属于当前库，切换知识库后不显示上一库错误", async () => {
  const view = setup();
  await act(async () => {
    await view.result.current.organize
      .perform("batch", async () => {
        throw new Error("保存冲突");
      })
      .catch(() => {});
  });
  await waitFor(() =>
    expect(view.result.current.organize.error).toBeInstanceOf(Error),
  );
  view.rerender({ id: "b" });
  expect(view.result.current.organize.error).toBeNull();
});
it("同一整理命令等待时不会再次发送，结束后清除pending", async () => {
  const view = setup();
  let complete: (() => void) | undefined;
  let save: Promise<void> | undefined;
  let calls = 0;
  act(() => {
    save = view.result.current.organize.perform(
      "batch",
      () =>
        new Promise<void>((resolve) => {
          calls++;
          complete = resolve;
        }),
    );
  });
  await waitFor(() => expect(view.result.current.organize.pending).toBe(true));
  await act(async () => {
    await view.result.current.organize.perform("batch", async () => {
      calls++;
    });
  });
  expect(calls).toBe(1);
  await act(async () => {
    complete?.();
    await save;
  });
  await waitFor(() => expect(view.result.current.organize.pending).toBe(false));
});
it("整理返回500未知结果时沿用原请求身份核验", async () => {
  const view = setup();
  const ids: string[] = [];
  await act(async () => {
    await view.result.current.organize
      .perform("same-payload", async (requestId) => {
        ids.push(requestId);
        throw new ApiError("整理接纳待核验", "Unknown", 500);
      })
      .catch(() => {});
  });
  await act(async () => {
    await view.result.current.organize.perform(
      "same-payload",
      async (requestId) => {
        ids.push(requestId);
      },
    );
  });
  expect(ids).toHaveLength(2);
  expect(ids[1]).toBe(ids[0]);
});
