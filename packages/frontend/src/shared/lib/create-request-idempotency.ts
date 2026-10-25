type StoredClientRequest = {
  fingerprint: string;
  id: string;
};

const storageKeyPrefix = "ally:create-request:";
const inMemoryRequests = new Map<string, StoredClientRequest>();

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSON).join(",")}]`;
  }
  if (value && typeof value === "object") {
    return `{${Object.entries(value)
      .filter(([, entry]) => entry !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entry]) => `${JSON.stringify(key)}:${canonicalJSON(entry)}`)
      .join(",")}}`;
  }
  return JSON.stringify(value) ?? "null";
}

function fingerprintOf(request: object) {
  const content = canonicalJSON(request);
  let first = 0x811c9dc5;
  let second = 0x9e3779b9;
  for (let index = 0; index < content.length; index += 1) {
    const code = content.charCodeAt(index);
    first = Math.imul(first ^ code, 0x01000193);
    second = Math.imul(second ^ code, 0x85ebca6b);
  }
  return `${content.length}:${(first >>> 0).toString(16)}:${(second >>> 0).toString(16)}`;
}

function storageKey(scopeKey: string) {
  return `${storageKeyPrefix}${scopeKey}`;
}

/** 为同一草稿和同一内容复用首发标识，并在内容变化时自动换键。 */
export function withStableCreateRequest<T extends object>(
  scopeKey: string,
  request: T,
): T & { clientRequestId: string } {
  const key = storageKey(scopeKey);
  const fingerprint = fingerprintOf(request);
  let stored = inMemoryRequests.get(key);
  try {
    const raw = window.localStorage.getItem(key);
    if (raw) stored = JSON.parse(raw) as StoredClientRequest;
  } catch {
    // localStorage 不可读时继续使用当前页面生命周期内的稳定记录。
  }
  const id =
    stored?.fingerprint === fingerprint &&
    typeof stored.id === "string" &&
    stored.id.length > 0 &&
    stored.id.length <= 64 &&
    stored.id
      ? stored.id
      : crypto.randomUUID();
  const next = { fingerprint, id } satisfies StoredClientRequest;
  inMemoryRequests.set(key, next);
  try {
    window.localStorage.setItem(key, JSON.stringify(next));
  } catch {
    // 本地存储不可写时仍由模块内存记录保证本次页面内稳定复用。
  }
  return { ...request, clientRequestId: id };
}

/** 首发得到确定成功响应后删除草稿幂等记录。 */
export function clearStableCreateRequest(scopeKey: string) {
  const key = storageKey(scopeKey);
  inMemoryRequests.delete(key);
  try {
    window.localStorage.removeItem(key);
  } catch {
    // 清理失败只会让相同草稿继续重放既有资源，不会重复创建。
  }
}
