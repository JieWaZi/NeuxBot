import { messageCompanionActivity } from "../runtime/companion-activity";
import type { CompanionActivity } from "@/shared/companion/activity";
import { getLivePartMutations } from "../runtime/chat-live";
import type { AllyChatMessage } from "../runtime/chat-projection";
import type { GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessageDisplayPart as DisplayPart } from "@/shared/api/generated/types.gen";

/** 节点只索引当前权威窗口的部件；不复制正文、工具参数或持久消息事实。 */
export type ChatPartNode = { readonly key: string; readonly part: DisplayPart };
export type ChatToolGroup = {
  readonly key: string;
  readonly kind: "tools";
  readonly nodes: readonly ChatPartNode[];
};
export type ChatProcessNode = ChatPartNode | ChatToolGroup;
export type ChatProcessGroup = {
  readonly key: string;
  readonly kind: "process";
  readonly nodes: readonly ChatProcessNode[];
};
export type ChatMessageSegment = ChatPartNode | ChatProcessGroup;
export type ChatMessageView = {
  readonly key: string;
  readonly message: AllyChatMessage;
  readonly segments: readonly ChatMessageSegment[];
  readonly activity: CompanionActivity;
};

export function isTerminalMessage(message: AllyChatMessage): boolean {
  return (
    message.status === "completed" ||
    message.status === "failed" ||
    message.status === "canceled"
  );
}
export function viewCompanionActivity(
  view: ChatMessageView,
  latest: boolean,
  interaction?: "waiting" | "stopping",
): CompanionActivity {
  if (!latest) return "idle";
  if (isTerminalMessage(view.message)) return view.activity;
  if (interaction) return "idle";
  return view.activity;
}
function equalEntries<T>(
  first: readonly T[] | undefined,
  second: readonly T[],
): boolean {
  return (
    first?.length === second.length &&
    second.every((entry, index) => first[index] === entry)
  );
}
/** 与 Harness dirty-node 思路相同，只让被改变的部件及其所属组获得新身份。 */
export class ChatMessageProjection {
  private messages = new Map<string, ChatMessageView>();
  private readonly textPositions = new WeakMap<
    ChatMessageView,
    ReadonlyMap<string, { source: number; segment?: number }>
  >();
  constructor(
    private readonly measured?: {
      node?: () => void;
      group?: () => void;
      inspect?: () => void;
    },
    private readonly scope = "",
  ) {}
  update(messages: readonly AllyChatMessage[]): readonly ChatMessageView[] {
    // 接纳回执已把临时 ID 换成权威 ID，后续快照未必继续携带 submissionId。
    // 只沿上一帧窗口内的角色与消息 ID 继承展示身份，窗口退出后不保留别名。
    const previousByID = new Map<string, ChatMessageView>();
    for (const view of this.messages.values()) {
      if (view.message.id)
        previousByID.set(
          `${view.message.role ?? "unknown"}:${view.message.id}`,
          view,
        );
    }
    const retained = new Map<string, ChatMessageView>();
    const views = messages.flatMap((message) => {
      const identity = message.submissionId || message.id;
      const proposedKey = identity
        ? `${this.scope}:${message.role ?? "unknown"}:${identity}`
        : undefined;
      if (!proposedKey) return [];
      const previous =
        previousByID.get(`${message.role ?? "unknown"}:${message.id}`) ??
        this.messages.get(proposedKey);
      const key = previous?.key ?? proposedKey;
      const view =
        previous?.message === message
          ? previous
          : this.message(key, message, previous);
      retained.set(key, view);
      return [view];
    });
    this.messages = retained;
    return views;
  }
  private message(
    key: string,
    message: AllyChatMessage,
    previous?: ChatMessageView,
  ): ChatMessageView {
    if (previous) {
      if (message.displayParts === previous.message.displayParts) {
        const view = {
          ...previous,
          message,
          activity:
            previous.message.status === message.status
              ? previous.activity
              : messageCompanionActivity({ ...message, isLast: true }),
        };
        const positions = this.textPositions.get(previous);
        if (positions) this.textPositions.set(view, positions);
        return view;
      }
      const fast = this.textAppend(key, message, previous);
      if (fast) return fast;
    }
    const leaves = new Map<string, ChatPartNode>();
    const groups = new Map<string, ChatToolGroup | ChatProcessGroup>();
    for (const segment of previous?.segments ?? []) {
      if (!("kind" in segment)) {
        leaves.set(segment.key, segment);
        continue;
      }
      groups.set(segment.key, segment);
      for (const child of segment.nodes) {
        if (!("kind" in child)) leaves.set(child.key, child);
        else {
          groups.set(child.key, child);
          for (const leaf of child.nodes) leaves.set(leaf.key, leaf);
        }
      }
    }
    const positions = new Map<string, { source: number; segment?: number }>();
    const parts = (message.displayParts ?? []).flatMap((part, index) => {
      this.measured?.inspect?.();
      if (part.type === "text" && part.id)
        positions.set(part.id, { source: index });
      if (part.type !== "tool" && !part.text?.trim()) return [];
      if (
        !["text", "reasoning", "process_commentary", "tool"].includes(
          part.type ?? "",
        )
      )
        return [];
      const nodeKey = `${key}:part:${part.id || index}`;
      const old = leaves.get(nodeKey);
      if (old?.part === part) return [old];
      this.measured?.node?.();
      return [{ key: nodeKey, part }];
    });
    const segments: ChatMessageSegment[] = [];
    let process: ChatProcessNode[] = [];
    let tools: ChatPartNode[] = [];
    const flushTools = () => {
      const first = tools[0];
      if (!first) return;
      const groupKey = `${first.key}:tools`;
      const old = groups.get(groupKey);
      if (old?.kind === "tools" && equalEntries(old.nodes, tools))
        process.push(old);
      else {
        this.measured?.group?.();
        process.push({ key: groupKey, kind: "tools", nodes: tools });
      }
      tools = [];
    };
    const flushProcess = () => {
      flushTools();
      const first = process[0];
      if (!first) return;
      const groupKey = `${first.key}:process`;
      const old = groups.get(groupKey);
      if (old?.kind === "process" && equalEntries(old.nodes, process))
        segments.push(old);
      else {
        this.measured?.group?.();
        segments.push({ key: groupKey, kind: "process", nodes: process });
      }
      process = [];
    };
    for (const node of parts) {
      if (node.part.type === "text") {
        flushProcess();
        segments.push(node);
      } else if (node.part.type === "tool") tools.push(node);
      else {
        flushTools();
        process.push(node);
      }
    }
    flushProcess();
    const view: ChatMessageView = {
      key,
      message,
      activity: messageCompanionActivity({ ...message, isLast: true }),
      segments:
        previous && equalEntries(previous.segments, segments)
          ? previous.segments
          : segments,
    };
    for (const [index, segment] of view.segments.entries()) {
      if (
        !("kind" in segment) &&
        segment.part.type === "text" &&
        segment.part.id
      ) {
        const position = positions.get(segment.part.id);
        if (position)
          positions.set(segment.part.id, { ...position, segment: index });
      }
    }
    this.textPositions.set(view, positions);
    return view;
  }
  /** 已证明连续的正文增量只查其索引，工具节点、子组与摘要不进入此路径。 */
  private textAppend(
    key: string,
    message: AllyChatMessage,
    previous: ChatMessageView,
  ): ChatMessageView | undefined {
    const current = message.displayParts;
    const before = previous.message.displayParts;
    if (!current || !before) return undefined;
    const changes = getLivePartMutations(current, before);
    const oldPositions = this.textPositions.get(previous);
    if (
      !changes ||
      !oldPositions ||
      changes.some(
        (change) => change.kind !== "text_append" || change.partType !== "text",
      )
    )
      return undefined;
    const ids = new Set(changes.map((change) => change.partId));
    const replacements: {
      id: string;
      source: number;
      segment?: number;
      part: DisplayPart;
    }[] = [];
    let added = 0;
    for (const id of ids) {
      const position = oldPositions.get(id);
      const source = position?.source ?? before.length + added++;
      const part = current[source];
      if (part?.id !== id || part.type !== "text") return undefined;
      // 既有空白段在中间重新出现需要重新分组，只有末尾能直接补入。
      if (position?.segment === undefined && source !== current.length - 1)
        return undefined;
      replacements.push({ id, source, segment: position?.segment, part });
    }
    if (before.length + added !== current.length) return undefined;
    let segments = previous.segments;
    let positions = oldPositions;
    for (const replacement of replacements) {
      this.measured?.inspect?.();
      if (!replacement.part.text?.trim()) continue;
      const node = {
        key: `${key}:part:${replacement.id}`,
        part: replacement.part,
      };
      this.measured?.node?.();
      if (replacement.segment !== undefined) {
        const next = [...segments];
        next[replacement.segment] = node;
        segments = next;
      } else {
        positions = new Map(positions).set(replacement.id, {
          source: replacement.source,
          segment: segments.length,
        });
        segments = [...segments, node];
      }
    }
    const working =
      previous.activity === "working" ||
      previous.activity === "searching" ||
      previous.activity === "connecting";
    const activity = isTerminalMessage(message)
      ? messageCompanionActivity({ ...message, isLast: true })
      : working ||
          !replacements.some((replacement) => replacement.part.text?.trim())
        ? previous.activity
        : "responding";
    const view = { key, message, segments, activity };
    this.textPositions.set(view, positions);
    return view;
  }
}
