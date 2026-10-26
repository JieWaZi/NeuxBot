function isReactMeasure(entry: PerformanceEntry): boolean {
  const detail: unknown = "detail" in entry ? entry.detail : undefined;
  if (!detail || typeof detail !== "object" || !("devtools" in detail))
    return false;
  const devtools = detail.devtools;
  if (!devtools || typeof devtools !== "object") return false;
  return (
    ("track" in devtools && devtools.track === "Components ⚛") ||
    ("trackGroup" in devtools && devtools.trackGroup === "Scheduler ⚛")
  );
}

/** 释放 React 开发构建的原生计时记录；性能录制已收到条目，业务测量和启动标记保留。 */
export function observeReactPerformanceMeasures(): () => void {
  if (
    !import.meta.env.DEV ||
    typeof PerformanceObserver === "undefined" ||
    !PerformanceObserver.supportedEntryTypes.includes("measure")
  )
    return () => {};

  const release = (entries: readonly PerformanceEntry[]) => {
    const names = new Set<string>();
    for (const entry of entries) {
      if (isReactMeasure(entry)) names.add(entry.name);
    }
    // React 19.2 每次组件更新都会写入 User Timing，浏览器不会自动淘汰这些记录。
    for (const name of names) {
      // User Timing 只能按名称删除；遇到同名业务测量时交还给业务所有者清理。
      if (performance.getEntriesByName(name, "measure").every(isReactMeasure))
        performance.clearMeasures(name);
    }
  };
  const observer = new PerformanceObserver((list) =>
    release(list.getEntries()),
  );
  observer.observe({ type: "measure", buffered: true });
  return () => {
    release(observer.takeRecords());
    observer.disconnect();
  };
}
