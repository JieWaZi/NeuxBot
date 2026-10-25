import {
  queryOptions,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { useEffect } from "react";
import { getAiAccountLogin } from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { subscribeGlobalEvents } from "@/shared/api/global-events";
import type { GithubComJieWaZiNeuxBotServerServicesAiaccountLogin as Login } from "@/shared/api/generated/types.gen";

/** 读取 Core 作业快照，Query 本身不推进官方授权，也没有周期请求。 */
export function accountLoginQueryOptions(
  accountId: string,
  login?: Login,
  queryClient?: QueryClient,
) {
  const queryKey = ["ai-account-login", accountId, login?.id];
  return queryOptions({
    queryKey,
    queryFn: async ({ signal }) => {
      const { data } = await getAiAccountLogin(
        withLocalErrorSurface({
          path: { accountId, loginId: login?.id ?? "" },
          signal,
          throwOnError: true,
        }),
      );
      // GET 与推送并发时，较早的等待快照不能盖掉已到达的终态。
      const current = queryClient?.getQueryData<Login>(queryKey);
      return current?.complete || current?.errorCode ? current : data;
    },
    enabled: Boolean(login?.id),
    initialData: login,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

function safeLogin(value: unknown): value is Login {
  if (typeof value !== "object" || value === null) return false;
  return (
    "id" in value &&
    typeof value.id === "string" &&
    "verificationUrl" in value &&
    typeof value.verificationUrl === "string" &&
    "userCode" in value &&
    typeof value.userCode === "string" &&
    "expiresAt" in value &&
    typeof value.expiresAt === "number" &&
    "intervalMs" in value &&
    typeof value.intervalMs === "number" &&
    "complete" in value &&
    typeof value.complete === "boolean" &&
    (!("errorCode" in value) || typeof value.errorCode === "string")
  );
}

/** 账号/授权任务订阅由全局通道共享；关闭只释放订阅，业务取消仍由原接口处理。 */
export function useAccountLoginQuery(
  accountId: string,
  login: Login | undefined,
  enabled: boolean,
) {
  const queryClient = useQueryClient();
  const options = accountLoginQueryOptions(accountId, login, queryClient);
  const query = useQuery({ ...options, enabled: options.enabled && enabled });
  const loginId = login?.id;
  useEffect(() => {
    if (!enabled || !loginId) return;
    const queryKey = ["ai-account-login", accountId, loginId];
    return subscribeGlobalEvents(
      "account-login",
      { kind: "account", id: accountId, parentId: loginId },
      {
        onEvent: (frame) => {
          const payload = frame.payload;
          if (
            frame.type !== "account-login.updated" ||
            typeof payload !== "object" ||
            payload === null ||
            !("login" in payload) ||
            !safeLogin(payload.login) ||
            payload.login.id !== loginId
          )
            return;
          const update = payload.login;
          queryClient.setQueryData<Login>(queryKey, (current) =>
            current?.complete || current?.errorCode ? current : update,
          );
        },
        onResync: () => {
          void queryClient.invalidateQueries({ queryKey });
        },
      },
    );
  }, [accountId, enabled, loginId, queryClient]);
  return { query, queryKey: options.queryKey };
}
