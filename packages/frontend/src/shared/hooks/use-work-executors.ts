import { useQuery } from "@tanstack/react-query";
import { listChannels, listWakers } from "@/shared/api/generated/sdk.gen";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWaker as Waker,
  GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as Team,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";

/** 选择执行者时完整加载执行者目录，避免把服务端第一页误当成全部伙伴和团队。 */
export function useWorkExecutors() {
  return useQuery({
    queryKey: ["work-executors"],
    queryFn: async ({ signal }) => {
      const wakers: Waker[] = [];
      const teams: Team[] = [];
      await Promise.all([
        (async () => {
          for (let pageNum = 1; ; pageNum++) {
            const { data } = await listWakers(
              withLocalErrorSurface({
                query: { pageNum, pageSize: 100 },
                signal,
                throwOnError: true,
              }),
            );
            const page = data?.resources || [];
            wakers.push(...page);
            if (
              !page.length ||
              wakers.length >= (data?.totalSize ?? wakers.length)
            )
              break;
          }
        })(),
        (async () => {
          for (let pageNum = 1; ; pageNum++) {
            const { data } = await listChannels(
              withLocalErrorSurface({
                query: { pageNum, pageSize: 100 },
                signal,
                throwOnError: true,
              }),
            );
            const page = data?.resources || [];
            teams.push(...page);
            if (
              !page.length ||
              teams.length >= (data?.totalSize ?? teams.length)
            )
              break;
          }
        })(),
      ]);
      return { wakers, teams };
    },
  });
}
