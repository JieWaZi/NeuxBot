import type { ChatLiveFrame, ChatMessageIncrement } from "@/shared/api/events";
import type { AllyChatMessage } from "./chat-projection";

/** 只保存活动流的顺序元数据，不复制正文和持久历史。 */
export class ChatLiveTracker {
  private readonly streams = new Map<
    string,
    { generation: string; sequence: number; revision: number }
  >();

  accept(
    frame: ChatLiveFrame,
    baseline: AllyChatMessage | undefined,
  ): "accept" | "duplicate" | "recover" {
    if (
      !baseline ||
      (baseline.executionId && baseline.executionId !== frame.executionId)
    )
      return "recover";
    if (
      ["completed", "failed", "canceled"].includes(baseline.status ?? "") &&
      (baseline.liveRevision ?? baseline.revision ?? -1) >= frame.revision
    )
      return "duplicate";
    const previous = this.streams.get(frame.messageId);
    if (previous) {
      if (previous.generation !== frame.generation) return "recover";
      if (frame.sequence <= previous.sequence) return "duplicate";
      if (frame.sequence !== previous.sequence + 1) return "recover";
    }
    let revision = Math.max(
      baseline.liveRevision ?? baseline.revision ?? -1,
      previous?.revision ?? -1,
    );
    for (const update of frame.updates) {
      if (update.revision <= revision) continue;
      if (update.baseRevision !== revision) return "recover";
      revision = update.revision;
    }
    this.streams.set(frame.messageId, {
      generation: frame.generation,
      sequence: frame.sequence,
      revision,
    });
    return revision <= (baseline.liveRevision ?? baseline.revision ?? -1)
      ? "duplicate"
      : "accept";
  }
}

const indexes = new WeakMap<object, ReadonlyMap<string, number>>();
type Parts = NonNullable<AllyChatMessage["displayParts"]>;
type Operation = NonNullable<ChatMessageIncrement["parts"]>[number];
const mutations = new WeakMap<
  Parts,
  { previous: WeakRef<Parts>; operations: readonly Operation[] }
>();

/** 派生节点可按活动流的脏部件更新；弱引用允许已消费的中间数组被释放。 */
export function getLivePartMutations(
  parts: Parts,
  baseline: Parts,
): readonly Operation[] | undefined {
  const batches: (readonly Operation[])[] = [];
  let current: Parts | undefined = parts;
  for (let depth = 0; current && depth < 256; depth++) {
    if (current === baseline) return batches.reverse().flat();
    const mutation = mutations.get(current);
    if (!mutation) return undefined;
    batches.push(mutation.operations);
    current = mutation.previous.deref();
  }
  return undefined;
}

/** 将已校验的临时增量折叠到当前消息，未变化部件保留引用。 */
export function applyChatLiveFrame(
  message: AllyChatMessage,
  frame: ChatLiveFrame,
): AllyChatMessage {
  let current = message;
  for (const update of frame.updates) {
    if (update.revision <= (current.liveRevision ?? current.revision ?? -1))
      continue;
    if (update.baseRevision !== (current.liveRevision ?? current.revision))
      return current;
    const previousParts = current.displayParts ?? [];
    let partIndexes = indexes.get(previousParts);
    if (!partIndexes) {
      partIndexes = new Map(
        previousParts.map((part, index) => [part.id ?? "", index]),
      );
      indexes.set(previousParts, partIndexes);
    }
    let parts = previousParts;
    if (update.parts?.length) {
      parts = [...previousParts];
      for (const operation of update.parts) {
        let index = partIndexes.get(operation.partId);
        if (index === undefined) {
          index = parts.length;
          partIndexes = new Map([...partIndexes, [operation.partId, index]]);
        }
        const previous = parts[index];
        switch (operation.kind) {
          case "text_append":
            parts[index] = {
              ...previous,
              id: operation.partId,
              type: operation.partType,
              text: (previous?.text ?? "") + (operation.text ?? ""),
            };
            break;
          case "part_type":
            if (previous)
              parts[index] = { ...previous, type: operation.partType };
            break;
          case "part_upsert":
            if (operation.part) parts[index] = operation.part;
            break;
        }
      }
      indexes.set(parts, partIndexes);
      mutations.set(parts, {
        previous: new WeakRef(previousParts),
        operations: update.parts,
      });
    }
    const bodyAppend = (update.parts ?? [])
      .filter(
        (part) =>
          part.kind === "text_append" &&
          (part.partType === "text" || part.partType === "process_commentary"),
      )
      .map((part) => part.text ?? "")
      .join("");
    current = {
      ...current,
      liveRevision: update.revision,
      revision: Math.max(current.revision ?? 0, update.revision),
      updatedAt: Math.max(current.updatedAt ?? 0, update.updatedAt),
      content: (current.content ?? "") + bodyAppend,
      displayParts: parts,
      ...(update.runMetrics ? { runMetrics: update.runMetrics } : {}),
    };
  }
  return current;
}
