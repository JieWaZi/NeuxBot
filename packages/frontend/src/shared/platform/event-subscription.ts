import { logger } from "@/shared/diagnostics/logger";

/** 将异步桌面监听的注册和注销绑定到一次组件订阅生命周期。 */
export function subscribeDesktopEvent<T>(
  event: string,
  register: (handler: (payload: T) => void) => Promise<() => void>,
  onEvent: (payload: T) => void,
): () => void {
  let active = true;
  let unsubscribe: (() => void) | undefined;
  const report = (phase: string, error: unknown) => {
    try {
      logger.error("desktop-event.failed", {
        event,
        phase,
        errorType: error instanceof Error ? error.name : typeof error,
      });
    } catch {
      // 诊断输出故障不能中断其他监听的资源释放。
    }
  };
  const release = async (cleanup: () => void) => {
    try {
      await cleanup();
    } catch (error) {
      report("unsubscribe", error);
    }
  };
  void Promise.resolve().then(async () => {
    // StrictMode 的模拟卸载无需创建一批随即废弃的原生监听。
    if (!active) return;
    try {
      const cleanup = await register((payload) => {
        if (active) onEvent(payload);
      });
      if (active) unsubscribe = cleanup;
      else await release(cleanup);
    } catch (error) {
      report("subscribe", error);
    }
  });
  return () => {
    if (!active) return;
    active = false;
    if (unsubscribe) void release(unsubscribe);
    unsubscribe = undefined;
  };
}
