/** 仅用于测试：记录 DOM 每次提交后的可见状态，不依赖生产诊断日志。 */
export function observeRenderedState<T>(container: Node, read: () => T) {
  const samples = [read()];
  const observer = new MutationObserver(() => samples.push(read()));
  observer.observe(container, {
    subtree: true,
    childList: true,
    attributes: true,
    characterData: true,
  });
  return { samples, disconnect: () => observer.disconnect() };
}
