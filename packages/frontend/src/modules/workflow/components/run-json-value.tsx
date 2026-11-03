import { jsonText } from "../model/types";

/** 运行数据保留 JSON 结构；长内容在自身区域内滚动。 */
export function RunJsonValue({ value }: { value: unknown }) {
  return (
    <pre
      tabIndex={0}
      className="bg-card type-body focus-visible:ring-ring max-h-80 min-w-0 overflow-auto overscroll-contain rounded-lg p-3 font-mono whitespace-pre focus-visible:ring-2 focus-visible:outline-none"
    >
      <code>{jsonText(value)}</code>
    </pre>
  );
}
