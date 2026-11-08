import type { QueryClient } from "@tanstack/react-query";
export * from "@/shared/api/generated/@tanstack/react-query.gen";
export async function invalidatePluginQueries(client: QueryClient) {
  await client.invalidateQueries({
    predicate: (query) => {
      const descriptor = query.queryKey[0] as
        { _id?: string; query?: { type?: string } } | undefined;
      const id = descriptor?._id ?? "";
      if (id === "listResourceCategories")
        return descriptor?.query?.type === "plugin";
      // 候选只由精确引用事件更新，管理配置与授权的成功回调不能重取它。
      return (
        id !== "listWakerPluginReferences" &&
        (/Plugin/.test(id) || /Library(Skill|Connector)/.test(id))
      );
    },
  });
}
