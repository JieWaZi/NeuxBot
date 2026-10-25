/** 返回聊天中允许用户选择的、已启用且探测就绪的 Runtime。 */
export function selectableRuntimes<
  Runtime extends { enabled?: boolean; state?: string },
>(runtimes: readonly Runtime[]): Runtime[] {
  return runtimes.filter(
    (runtime) => runtime.enabled && runtime.state === "ready",
  );
}
