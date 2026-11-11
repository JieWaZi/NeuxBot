import type {
  GetWakerSkillResponse,
  GetWakerSkillError,
  GetSkillVersionResponse,
  GetSkillVersionError,
  GetSkillVersionFileResponse,
  GetSkillVersionFileError,
  ListSkillVersionsResponse,
  ListSkillVersionsError,
} from "@/shared/api/generated/types.gen";
import {
  getWakerSkillOptions as partnerGetWakerSkillOptions,
  getLibrarySkillOptions,
  getSkillVersionOptions as partnerGetSkillVersionOptions,
  getLibrarySkillVersionOptions,
  getSkillVersionFileOptions as partnerGetSkillVersionFileOptions,
  getLibrarySkillVersionFileOptions,
  listSkillVersionsOptions as partnerListSkillVersionsOptions,
  listLibrarySkillVersionsOptions,
  createSkillVersionMutation as partnerCreateSkillVersionMutation,
  createLibrarySkillVersionMutation,
  pullSkillUpdateMutation as partnerPullSkillUpdateMutation,
  pullLibrarySkillUpdateMutation,
  restoreSkillVersionMutation as partnerRestoreSkillVersionMutation,
  restoreLibrarySkillVersionMutation,
  uninstallSkillMutation as partnerUninstallSkillMutation,
  deleteLibrarySkillMutation,
  updateWakerSkillMutation as partnerUpdateWakerSkillMutation,
  updateLibrarySkillMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type {
  MutationFunctionContext,
  QueryClient,
  UseQueryOptions,
} from "@tanstack/react-query";

export {
  getSkillVersionQueryKey,
  getSkillVersionFileQueryKey,
  getWakerSkillQueryKey,
  listSkillVersionsQueryKey,
  listWakerSkillsOptions,
  listWakerSkillsQueryKey,
  setWakerSkillEnabledMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

const visibleSkillQueryIds = new Set([
  "listLibrarySkills",
  "getLibrarySkill",
  "listLibrarySkillVersions",
  "getLibrarySkillVersion",
  "getLibrarySkillVersionFile",
  "listWakerSkills",
  "getWakerSkill",
  "listSkillVersions",
  "getSkillVersion",
  "getSkillVersionFile",
]);

export async function invalidateVisibleSkillQueries(
  queryClient: QueryClient,
): Promise<void> {
  await queryClient.invalidateQueries({
    predicate: (query) => {
      const descriptor = query.queryKey[0] as
        { _id?: string; query?: { type?: string } } | undefined;
      if (descriptor?._id === "listResourceCategories")
        return descriptor.query?.type === "skill";
      return Boolean(
        descriptor?._id && visibleSkillQueryIds.has(descriptor._id),
      );
    },
  });
}

/** 全局详情使用真实资源身份，伙伴范围只决定安装上下文。 */
export function getWakerSkillOptions(
  options: Parameters<typeof partnerGetWakerSkillOptions>[0],
): UseQueryOptions<GetWakerSkillResponse, GetWakerSkillError> {
  if (!options) throw new Error("资源查询参数缺失");
  if (options.path.wakerId)
    return partnerGetWakerSkillOptions(options) as UseQueryOptions<
      GetWakerSkillResponse,
      GetWakerSkillError
    >;
  const resourcePath = { skillId: options.path.skillId };
  return getLibrarySkillOptions({
    ...options,
    path: resourcePath,
  }) as UseQueryOptions<GetWakerSkillResponse, GetWakerSkillError>;
}

/** 全局详情使用真实资源身份，伙伴范围只决定安装上下文。 */
export function getSkillVersionOptions(
  options: Parameters<typeof partnerGetSkillVersionOptions>[0],
): UseQueryOptions<GetSkillVersionResponse, GetSkillVersionError> {
  if (!options) throw new Error("资源查询参数缺失");
  if (options.path.wakerId)
    return partnerGetSkillVersionOptions(options) as UseQueryOptions<
      GetSkillVersionResponse,
      GetSkillVersionError
    >;
  const resourcePath = {
    skillId: options.path.skillId,
    versionId: options.path.versionId,
  };
  return getLibrarySkillVersionOptions({
    ...options,
    path: resourcePath,
  }) as UseQueryOptions<GetSkillVersionResponse, GetSkillVersionError>;
}

/** 全局详情使用真实资源身份，伙伴范围只决定安装上下文。 */
export function getSkillVersionFileOptions(
  options: Parameters<typeof partnerGetSkillVersionFileOptions>[0],
): UseQueryOptions<GetSkillVersionFileResponse, GetSkillVersionFileError> {
  if (!options) throw new Error("资源查询参数缺失");
  if (options.path.wakerId)
    return partnerGetSkillVersionFileOptions(options) as UseQueryOptions<
      GetSkillVersionFileResponse,
      GetSkillVersionFileError
    >;
  const resourcePath = {
    skillId: options.path.skillId,
    versionId: options.path.versionId,
  };
  return getLibrarySkillVersionFileOptions({
    ...options,
    path: resourcePath,
  }) as UseQueryOptions<GetSkillVersionFileResponse, GetSkillVersionFileError>;
}

/** 全局详情使用真实资源身份，伙伴范围只决定安装上下文。 */
export function listSkillVersionsOptions(
  options: Parameters<typeof partnerListSkillVersionsOptions>[0],
): UseQueryOptions<ListSkillVersionsResponse, ListSkillVersionsError> {
  if (!options) throw new Error("资源查询参数缺失");
  if (options.path.wakerId)
    return partnerListSkillVersionsOptions(options) as UseQueryOptions<
      ListSkillVersionsResponse,
      ListSkillVersionsError
    >;
  const resourcePath = { skillId: options.path.skillId };
  return listLibrarySkillVersionsOptions({
    ...options,
    path: resourcePath,
  }) as UseQueryOptions<ListSkillVersionsResponse, ListSkillVersionsError>;
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function createSkillVersionMutation(
  options?: Parameters<typeof partnerCreateSkillVersionMutation>[0],
) {
  const partner = partnerCreateSkillVersionMutation(options);
  const library = createLibrarySkillVersionMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { skillId: input.path.skillId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function pullSkillUpdateMutation(
  options?: Parameters<typeof partnerPullSkillUpdateMutation>[0],
) {
  const partner = partnerPullSkillUpdateMutation(options);
  const library = pullLibrarySkillUpdateMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { skillId: input.path.skillId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function restoreSkillVersionMutation(
  options?: Parameters<typeof partnerRestoreSkillVersionMutation>[0],
) {
  const partner = partnerRestoreSkillVersionMutation(options);
  const library = restoreLibrarySkillVersionMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = {
        skillId: input.path.skillId,
        versionId: input.path.versionId,
      };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function uninstallSkillMutation(
  options?: Parameters<typeof partnerUninstallSkillMutation>[0],
) {
  const partner = partnerUninstallSkillMutation(options);
  const library = deleteLibrarySkillMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: (
      input: {
        path: { wakerId: string; skillId: string };
        query?: { confirmed?: boolean; expectedAffectedCount?: number };
      },
      context: MutationFunctionContext,
    ) => {
      if (input.path.wakerId) return partnerFn({ path: input.path }, context);
      return libraryFn(
        { path: { skillId: input.path.skillId }, query: input.query },
        context,
      );
    },
  };
}

/** 同一专业表单按资源入口提交，不复制伙伴或共享设置。 */
export function updateWakerSkillMutation(
  options?: Parameters<typeof partnerUpdateWakerSkillMutation>[0],
) {
  const partner = partnerUpdateWakerSkillMutation(options);
  const library = updateLibrarySkillMutation();
  const partnerFn = partner.mutationFn;
  const libraryFn = library.mutationFn;
  if (!partnerFn || !libraryFn) throw new Error("资源操作尚未配置");
  return {
    ...partner,
    mutationFn: ((input, context) => {
      if (input.path.wakerId) return partnerFn(input, context);
      const resourcePath = { skillId: input.path.skillId };
      return libraryFn({ ...input, path: resourcePath }, context);
    }) as NonNullable<typeof partner.mutationFn>,
  };
}
