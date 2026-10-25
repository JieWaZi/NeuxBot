import {
  createContext,
  useContext,
  useCallback,
  useState,
  type ComponentType,
  type ReactNode,
} from "react";
import type { WorkbenchContentReference } from "../workbench/workbench-model";
import { useWorkbench } from "../workbench/workbench-store";

export type ChatContributionScopeProps = {
  chatId?: string;
  scopeKey: string;
  isRunning: boolean;
  active?: boolean;
  enabled?: boolean;
  registerMessages: (render: (messageId: string) => ReactNode) => () => void;
};
export type WorkbenchContributionProps = {
  reference: WorkbenchContentReference;
  active: boolean;
  scopeKey: string;
};
export type ChatContributions = {
  Scope?: ComponentType<ChatContributionScopeProps>;
  Content?: ComponentType<WorkbenchContributionProps>;
};
const Contributions = createContext<ChatContributions>({});
const MessageContent = createContext<
  ((messageId: string) => ReactNode) | undefined
>(undefined);
export const ChatMessageContributionProvider = MessageContent.Provider;
export const ChatContributionsProvider = Contributions.Provider;
const WorkbenchOpening = createContext<
  { scopeKey: string; onOpen: () => void } | undefined
>(undefined);
export const WorkbenchOpeningProvider = WorkbenchOpening.Provider;

/** 嵌入式输入复用所属聊天的真实工作范围及工作台打开入口。 */
export function useChatWorkspaceHost() {
  return useContext(WorkbenchOpening);
}

/** 领域模块通过公开贡献槽提供消息内容；Chat 不依赖领域数据或实现。 */
export function ChatMessageContribution({ messageId }: { messageId: string }) {
  const render = useContext(MessageContent);
  return render?.(messageId) ?? null;
}
export function ChatContributionScope({
  children,
  ...props
}: Omit<ChatContributionScopeProps, "registerMessages"> & {
  children: ReactNode;
}) {
  const { Scope } = useContext(Contributions);
  const [registered, setRegistered] = useState<{
    scopeKey: string;
    render: (messageId: string) => ReactNode;
  }>();
  const registerMessages = useCallback(
    (render: (messageId: string) => ReactNode) => {
      setRegistered({ scopeKey: props.scopeKey, render });
      return () => setRegistered(undefined);
    },
    [props.scopeKey],
  );
  return (
    <MessageContent.Provider
      value={
        registered?.scopeKey === props.scopeKey ? registered.render : undefined
      }
    >
      {children}
      {Scope && props.enabled !== false ? (
        <Scope {...props} registerMessages={registerMessages} />
      ) : null}
    </MessageContent.Provider>
  );
}
export function WorkbenchContribution(props: WorkbenchContributionProps) {
  const { Content } = useContext(Contributions);
  return Content ? <Content {...props} /> : null;
}
export function useOpenWorkbenchContent(scopeKey: string) {
  const { dispatch } = useWorkbench(scopeKey);
  const opening = useContext(WorkbenchOpening);
  return (
    contentId: string,
    title: string,
    reference: WorkbenchContentReference,
  ) => {
    dispatch({ type: "open-content", contentId, title, reference });
    if (opening?.scopeKey === scopeKey) opening.onOpen();
  };
}
