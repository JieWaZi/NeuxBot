import type {
  GetWakerConnectorResponse,
  GetWakerConnectorError,
} from "@/shared/api/generated/types.gen";
import {
  getWakerConnectorOptions as partnerGetWakerConnectorOptions,
  getLibraryConnectorOptions,
  createWakerConnectorMutation as partnerCreateWakerConnectorMutation,
  createLibraryConnectorMutation,
  updateWakerConnectorMutation as partnerUpdateWakerConnectorMutation,
  updateLibraryConnectorMutation,
  deleteWakerConnectorMutation as partnerDeleteWakerConnectorMutation,
  deleteLibraryConnectorMutation,
  testWakerConnectorMutation as partnerTestWakerConnectorMutation,
  testLibraryConnectorMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { QueryClient, UseQueryOptions } from "@tanstack/react-query";

export {
  getWakerConnectorQueryKey,
  listWakerConnectorsOptions,
  listWakerConnectorsQueryKey,
  setWakerConnectorEnabledMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

const visibleConnectorQueryIDs = new Set([
  "listLibraryConnectors",
  "getLibraryConnector",
  "listWakerConnectors",
  "getWakerConnector",
]);

export async function invalidateVisibleConnectorQueries(
  queryClient: QueryClient,
): Promise<void> {
  await queryClient.invalidateQueries({
    predicate: (query) => {
      const descriptor = query.queryKey[0] as { _id?: string } | undefined;
      return Boolean(
        descriptor?._id && visibleConnectorQueryIDs.has(descriptor._id),
      );
    },
  });
}

/** 全局详情使用真实资源身份，伙伴范围只决定安装上下文。 */
export function getWakerConnectorOptions(
  options: Parameters<typeof partnerGetWakerConnectorOptions>[0],
): UseQueryOptions<GetWakerConnectorResponse, GetWakerConnectorError> {
  if (!options) throw new Error("资源查询参数缺失");
  if (options.path.wakerId)
    return partnerGetWakerConnectorOptions(options) as UseQueryOptions<
      GetWakerConnectorResponse,
      GetWakerConnectorError
    >;
  const resourcePath = { connectorId: options.path.connectorId };
  return getLibraryConnectorOptions({
    ...options,
    path: resourcePath,
  }) as UseQueryOptions<GetWakerConnectorResponse, GetWakerConnectorError>;
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function createWakerConnectorMutation(
  options?: Parameters<typeof partnerCreateWakerConnectorMutation>[0],
) {
  const partner = partnerCreateWakerConnectorMutation(options);
  const library = createLibraryConnectorMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const globalInput = { ...input, path: undefined };
      return libraryFn(globalInput, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function updateWakerConnectorMutation(
  options?: Parameters<typeof partnerUpdateWakerConnectorMutation>[0],
) {
  const partner = partnerUpdateWakerConnectorMutation(options);
  const library = updateLibraryConnectorMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { connectorId: input.path.connectorId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function deleteWakerConnectorMutation(
  options?: Parameters<typeof partnerDeleteWakerConnectorMutation>[0],
) {
  const partner = partnerDeleteWakerConnectorMutation(options);
  const library = deleteLibraryConnectorMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { connectorId: input.path.connectorId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function testWakerConnectorMutation(
  options?: Parameters<typeof partnerTestWakerConnectorMutation>[0],
) {
  const partner = partnerTestWakerConnectorMutation(options);
  const library = testLibraryConnectorMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { connectorId: input.path.connectorId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}
