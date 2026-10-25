import { deleteApiKey } from "./generated/sdk.gen";
import {
  getApiServiceOptions,
  getApiServiceQueryKey,
  updateApiServiceMutation,
  updateMcpServiceMutation,
} from "./generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
} from "./local-error-surface";

/** 设置页与自动任务共同读取同一个服务状态，不能派生另一份监听配置。 */
export const integrationServiceOptions = () =>
  getApiServiceOptions(withLocalErrorSurface({}));
export const integrationServiceQueryKey = getApiServiceQueryKey;
export const integrationServiceMutation = () =>
  withLocalErrorMutation(updateApiServiceMutation());

/** MCP 独立启停，复用发布服务状态与就地错误约定。 */
export const integrationMcpServiceMutation = () =>
  withLocalErrorMutation(updateMcpServiceMutation());

/** 删除固定版本的密钥；错误只显示在当前删除对话框。 */
export const deleteIntegrationKey = (keyId: string, expectedRevision: number) =>
  deleteApiKey<true>(
    withLocalErrorSurface({
      path: { keyId },
      body: { expectedRevision },
      throwOnError: true,
    }),
  );
