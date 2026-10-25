/** 把源码表达式变成可编辑模板；动态表达式保留为插值，避免手改丢失 args。 */
export function editablePrompt(source: string): string {
  if (source.startsWith("`") && source.endsWith("`"))
    return source.slice(1, -1);
  const concatenation = source.match(/^("(?:[^"\\]|\\.)*")\s*\+\s*([\s\S]+)$/);
  if (concatenation) {
    return `${literalText(concatenation[1] ?? "")}${"${"}${concatenation[2] ?? ""}}`;
  }
  if (source.startsWith('"') && source.endsWith('"')) {
    try {
      return literalText(source);
    } catch {
      /* Retain the expression below. */
    }
  }
  if (source.startsWith("'") && source.endsWith("'")) {
    return source.slice(1, -1).replace(/\\'/g, "'").replace(/\$\{/g, "\\${");
  }
  return source ? `${"${"}${source}}` : "";
}

function literalText(source: string): string {
  const value: unknown = JSON.parse(source);
  if (typeof value !== "string") throw new Error("prompt_literal_required");
  return value.replace(/\\/g, "\\\\").replace(/\$\{/g, "\\${");
}
