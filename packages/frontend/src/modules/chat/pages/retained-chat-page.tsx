import { useState, type ComponentProps } from "react";
import { ChatPageContent } from "./chat-page";

type ChatProps = ComponentProps<typeof ChatPageContent>;
type RetainedProps = ChatProps & {
  entry: NonNullable<ChatProps["entry"]>;
  draftBinding?: { scopeKey: string; chatId: string };
  closedChatIds?: readonly string[];
};
type RetainedInstance = { props: RetainedProps; instanceId: string };

/** 单个业务 Scene 内切换 Chat 时，保留与普通 Chat Scene 相同的实例生命周期。 */
export function RetainedChatPageContent(props: RetainedProps) {
  const [state, setState] = useState<{
    current: RetainedInstance;
    previous: RetainedInstance[];
  }>({ current: { props, instanceId: props.entry.scopeKey }, previous: [] });
  if (state.current.props !== props) {
    const sameScope =
      state.current.props.entry.scopeKey === props.entry.scopeKey;
    const bindingDraft =
      !state.current.props.chatId &&
      Boolean(props.chatId) &&
      ((props.draftBinding !== undefined &&
        props.draftBinding.chatId === props.chatId &&
        props.draftBinding.scopeKey === state.current.props.entry.scopeKey) ||
        state.current.props.entry.isHistoryLoading);
    const returning = state.previous.find(
      (item) => item.props.entry.scopeKey === props.entry.scopeKey,
    );
    setState({
      current: {
        props,
        instanceId:
          sameScope || bindingDraft
            ? state.current.instanceId
            : (returning?.instanceId ?? props.entry.scopeKey),
      },
      previous: (sameScope || bindingDraft
        ? state.previous
        : [
            ...state.previous.filter(
              (item) => item.props.entry.scopeKey !== props.entry.scopeKey,
            ),
            state.current,
          ]
      ).filter(
        (item) =>
          Boolean(item.props.chatId) &&
          !props.closedChatIds?.includes(item.props.chatId ?? ""),
      ),
    });
  }
  return (
    <div className="relative h-full min-h-0 min-w-0">
      {[...state.previous, state.current].map(({ props: item, instanceId }) => {
        const selected = item.entry.scopeKey === props.entry.scopeKey;
        return (
          <div
            key={instanceId}
            className={
              selected
                ? "absolute inset-0"
                : "pointer-events-none invisible absolute inset-0"
            }
            aria-hidden={!selected}
            inert={!selected}
          >
            <ChatPageContent
              {...item}
              entry={{
                ...item.entry,
                active: selected,
                history: selected ? item.entry.history : undefined,
                mainContent: selected
                  ? item.entry.mainContent
                  : item.entry.mainContent && {
                      ...item.entry.mainContent,
                      content: null,
                    },
              }}
            />
          </div>
        );
      })}
    </div>
  );
}
