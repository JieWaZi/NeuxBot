import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  clearStableCreateRequest,
  withStableCreateRequest,
} from "./create-request-idempotency";

describe("create request idempotency", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(crypto, "randomUUID")
      .mockReturnValueOnce("11111111-1111-4111-8111-111111111111")
      .mockReturnValueOnce("22222222-2222-4222-8222-222222222222")
      .mockReturnValueOnce("33333333-3333-4333-8333-333333333333");
  });

  it("跨组件重建为同一草稿和内容复用标识", () => {
    const request = {
      text: "实现团队功能",
      workingDirectory: "/tmp/project",
      memberConfigs: [],
    };
    const first = withStableCreateRequest("team:channel-1:draft:one", request);
    const retry = withStableCreateRequest("team:channel-1:draft:one", {
      memberConfigs: [],
      workingDirectory: "/tmp/project",
      text: "实现团队功能",
    });

    expect(retry.clientRequestId).toBe(first.clientRequestId);
  });

  it("内容变化或成功清理后生成新标识", () => {
    const scope = "chat:draft:one";
    const first = withStableCreateRequest(scope, {
      text: "实现团队功能",
      workingDirectory: "/tmp/project",
    });
    const changed = withStableCreateRequest(scope, {
      text: "实现另一个功能",
      workingDirectory: "/tmp/project",
    });
    clearStableCreateRequest(scope);
    const afterSuccess = withStableCreateRequest(scope, {
      text: "实现另一个功能",
      workingDirectory: "/tmp/project",
    });

    expect(changed.clientRequestId).not.toBe(first.clientRequestId);
    expect(afterSuccess.clientRequestId).not.toBe(changed.clientRequestId);
  });

  it("本地存储不可用时在当前页面内仍复用标识", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("storage denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("storage denied");
    });
    const scope = "chat:draft:storage-denied";
    const first = withStableCreateRequest(scope, { text: "继续工作" });
    const retry = withStableCreateRequest(scope, { text: "继续工作" });

    expect(retry.clientRequestId).toBe(first.clientRequestId);
    clearStableCreateRequest(scope);
  });
});
