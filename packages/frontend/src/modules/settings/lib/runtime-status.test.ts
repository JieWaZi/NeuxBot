import { expect, it } from "vitest";
import { runtimeHeaderState } from "./runtime-status";

it("程序可用但认证未知不能宣称已连接", () => {
  const runtime = {
    installed: true,
    enabled: true,
    state: "ready",
    auth: "unknown",
  };
  expect(runtimeHeaderState(runtime)).toEqual({
    tone: "idle",
    label: "available",
  });
  expect(runtimeHeaderState({ ...runtime, auth: "ok" })).toEqual({
    tone: "connected",
    label: "ready",
  });
  expect(runtimeHeaderState({ ...runtime, auth: "missing" })).toEqual({
    tone: "attention",
    label: "loginRequired",
  });
});
it("更新中的旧可用程序和失败更新明确呈现安装任务，未安装不显示连接成功", () => {
  const runtime = {
    installed: true,
    enabled: true,
    state: "ready",
    auth: "ok",
  };
  expect(runtimeHeaderState(runtime, { state: "installing" })).toEqual({
    tone: "connecting",
    label: "installing",
  });
  expect(runtimeHeaderState(runtime, { state: "failed" })).toEqual({
    tone: "attention",
    label: "installFailed",
  });
  expect(runtimeHeaderState({ ...runtime, installed: false })).toEqual({
    tone: "idle",
    label: "notInstalled",
  });
  expect(
    runtimeHeaderState(runtime, { state: "idle", installed: false }),
  ).toEqual({
    tone: "idle",
    label: "notInstalled",
  });
  expect(runtimeHeaderState({ ...runtime, enabled: false })).toEqual({
    tone: "unavailable",
    label: "disabled",
  });
});

it("程序删除与可恢复的删除失败不会冒充已连接", () => {
  const runtime = {
    installed: true,
    enabled: true,
    state: "ready",
    auth: "ok",
  };
  expect(
    runtimeHeaderState(runtime, {
      state: "installing",
      operation: "uninstall",
    }),
  ).toEqual({ tone: "connecting", label: "removing" });
  expect(
    runtimeHeaderState(runtime, {
      state: "failed",
      operation: "uninstall",
      installed: false,
    }),
  ).toEqual({ tone: "attention", label: "removalFailed" });
});

it("原生正在授权与本机凭据配置限制使用各自状态，不误导成无入口的登录", () => {
  const runtime = {
    installed: true,
    enabled: true,
    state: "ready",
    auth: "missing",
  };
  expect(
    runtimeHeaderState(runtime, undefined, { state: "authenticating" }),
  ).toEqual({ tone: "connecting", label: "signingIn" });
  expect(
    runtimeHeaderState(runtime, undefined, {
      state: "idle",
      canAuthenticate: false,
      configurationReason: "environment_credentials",
    }),
  ).toEqual({ tone: "attention", label: "configurationRequired" });
  expect(
    runtimeHeaderState({ ...runtime, defaultAccountId: "managed" }, undefined, {
      state: "idle",
      canAuthenticate: false,
      configurationReason: "environment_credentials",
    }).label,
  ).toBe("loginRequired");
});
