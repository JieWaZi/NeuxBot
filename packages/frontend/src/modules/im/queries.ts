import { useQueryEvents } from "@/shared/api/use-query-events";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  listImBindings,
  listImConnections,
  listImConversations,
  getImConnectionDependencies,
} from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import type { IMBinding } from "./types";

export const imQueryKeys = {
  all: ["im"] as const,
  connections: ["im", "connections"] as const,
  bindings: (connectionId: string) =>
    ["im", "connection-bindings", connectionId] as const,
  conversations: (connectionId: string, bindingIds: string[]) =>
    ["im", "conversations", connectionId, bindingIds] as const,
  login: (loginId?: string) => ["im", "login", loginId] as const,
  dependencies: (connectionId: string) =>
    ["im", "deletion-dependencies", connectionId] as const,
};

export function imConnectionDependencyOptions(connectionId: string) {
  return {
    queryKey: imQueryKeys.dependencies(connectionId),
    queryFn: async ({ signal }: { signal: AbortSignal }) =>
      (
        await getImConnectionDependencies(
          withLocalErrorSurface({
            path: { connectionId },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
  };
}

export function useIMConnections() {
  const active = useRouteSceneActive();
  const client = useQueryClient();
  useQueryEvents({
    topic: "im",
    resource: { kind: "global" },
    queryKeys: [imQueryKeys.connections],
    active,
  });
  const query = useQuery({
    queryKey: imQueryKeys.connections,
    queryFn: async ({ signal }) =>
      (
        await listImConnections(
          withLocalErrorSurface({ signal, throwOnError: true }),
        )
      ).data,
  });
  return {
    ...query,
    refresh: () => client.invalidateQueries({ queryKey: imQueryKeys.all }),
  };
}

export function useIMBindings(connectionId = "", enabled = true) {
  const active = useRouteSceneActive();
  useQueryEvents({
    topic: "im",
    resource: { kind: "connection", id: connectionId },
    queryKeys: [imQueryKeys.bindings(connectionId)],
    active,
    enabled: enabled && Boolean(connectionId),
  });
  return useQuery({
    queryKey: imQueryKeys.bindings(connectionId),
    queryFn: async ({ signal }) => {
      const resources: IMBinding[] = [];
      for (let offset = 0; ; offset += 50) {
        const { data } = await listImBindings(
          withLocalErrorSurface({
            signal,
            query: { connectionId, offset },
            throwOnError: true,
          }),
        );
        const page = data?.resources || [];
        resources.push(...page);
        if (
          !page.length ||
          resources.length >= (data?.totalSize ?? resources.length)
        )
          break;
      }
      return resources;
    },
    enabled,
  });
}

export function useIMConversations(
  connectionId: string,
  bindings: IMBinding[],
  enabled: boolean,
) {
  const active = useRouteSceneActive();
  useQueryEvents({
    topic: "im",
    resource: { kind: "connection", id: connectionId },
    queryKeys: [
      imQueryKeys.conversations(
        connectionId,
        bindings.flatMap((binding) => binding.id || []),
      ),
    ],
    active,
    enabled: enabled && Boolean(connectionId),
  });
  return useQuery({
    queryKey: imQueryKeys.conversations(
      connectionId,
      bindings.flatMap((binding) => binding.id || []),
    ),
    enabled,
    queryFn: async ({ signal }) =>
      (
        await Promise.all(
          bindings
            .filter((binding) => binding.id)
            .map(async (binding) => {
              const { data } = await listImConversations(
                withLocalErrorSurface({
                  path: { bindingId: binding.id || "" },
                  signal,
                  throwOnError: true,
                }),
              );
              return (data?.resources || []).map((conversation) => ({
                conversation,
                binding,
              }));
            }),
        )
      )
        .flat()
        .sort(
          (a, b) =>
            (b.conversation.createdAt || 0) - (a.conversation.createdAt || 0),
        ),
  });
}
