import { AssistantMarkdown } from "./chat-message-content";

/** 在执行结果中复用 Markdown 排版；文件引用由调用方检查后提供独立入口。 */
export function ReadOnlyMarkdown({ source }: { source: string }) {
  return (
    <AssistantMarkdown
      text={source}
      allowFileReferences={false}
      renderImage={({ alt }) => <span>{alt ?? ""}</span>}
    />
  );
}
