import type { QueryClient } from "@tanstack/react-query";
export * from "@/shared/api/generated/@tanstack/react-query.gen";
export async function invalidatePluginQueries(client: QueryClient) {
  await client.invalidateQueries({
    predicate: (query) => {
      const id = (query.queryKey[0] as { _id?: string } | undefined)?._id ?? "";
      return /Plugin/.test(id) || /Library(Skill|Connector)/.test(id);
    },
  });
}
