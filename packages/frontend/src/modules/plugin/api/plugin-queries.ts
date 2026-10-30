import type { QueryClient } from "@tanstack/react-query";
export * from "@/shared/api/generated/@tanstack/react-query.gen";
export async function invalidatePluginQueries(client: QueryClient) {
  await client.invalidateQueries({
    predicate: (query) => {
      const id = (query.queryKey[0] as { _id?: string } | undefined)?._id ?? "";
      // 候选只由精确引用事件更新，管理配置与授权的成功回调不能重取它。
      return (
        id !== "listWakerPluginReferences" &&
        (/Plugin/.test(id) || /Library(Skill|Connector)/.test(id))
      );
    },
  });
}
