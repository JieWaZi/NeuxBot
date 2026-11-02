import { useMessagePartText } from "@assistant-ui/react";
import {
  AssistantMarkdown,
  UserMarkdown,
  type AssistantMarkdownProps,
} from "@/modules/chat/components/chat-message-content";

/** 仅测试旧语料的文本输入；生产 Markdown 没有消息部件上下文依赖。 */
export function AssistantText(props: Omit<AssistantMarkdownProps, "text">) {
  const { text } = useMessagePartText();
  return <AssistantMarkdown {...props} text={text} />;
}
export function UserText() {
  const { text } = useMessagePartText();
  return <UserMarkdown text={text} />;
}
