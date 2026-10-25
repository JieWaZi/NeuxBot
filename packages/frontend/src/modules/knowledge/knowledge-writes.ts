import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import type { ResourceRef } from "./knowledge-transfer";
import { knowledgeKeys } from "./knowledge-queries";

export type KnowledgePerform = (
  key: string,
  action: (requestId: string) => Promise<unknown>,
) => Promise<void>;
/** 同一操作的未知结果保留请求身份；已接纳之后仅对账当前库。 */
export function useKnowledgeWrites(ref: ResourceRef) {
  const client = useQueryClient();
  const scope = JSON.stringify(knowledgeKeys.resource(ref));
  const [requests] = useState(() => new Map<string, string>());
  const [inFlight] = useState(() => new Set<string>());
  function useOperation(name: string) {
    const mutation = useMutation({
      mutationFn: async ({
        action,
      }: {
        scope: string;
        action: () => Promise<unknown>;
      }) => action(),
      retry: false,
    });
    const perform: KnowledgePerform = async (key, action) => {
      const identity = `${scope}:${name}:${key}`;
      if (inFlight.has(identity)) return;
      const requestId = requests.get(identity) ?? crypto.randomUUID();
      requests.set(identity, requestId);
      inFlight.add(identity);
      try {
        await mutation.mutateAsync({ scope, action: () => action(requestId) });
        requests.delete(identity);
        await Promise.all([
          client.invalidateQueries({ queryKey: knowledgeKeys.resource(ref) }),
        ]);
      } catch (cause) {
        if (!isKnowledgeWriteOutcomeUnknown(cause)) requests.delete(identity);
        throw cause;
      } finally {
        inFlight.delete(identity);
      }
    };
    return {
      perform,
      pending: mutation.variables?.scope === scope && mutation.isPending,
      error: mutation.variables?.scope === scope ? mutation.error : null,
      reset: mutation.reset,
    };
  }
  const organize = useOperation("organize");
  return { organize };
}
