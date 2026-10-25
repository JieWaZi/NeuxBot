import {
  listApiKeysOptions,
  listApiKeysQueryKey,
  listPublishableResourcesQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
export const apiKeysOptions = (pageNum: number, pageSize: number) =>
  listApiKeysOptions(withLocalErrorSurface({ query: { pageNum, pageSize } }));
export const apiKeysQueryKey = listApiKeysQueryKey;
export const publishableResourcesKey = listPublishableResourcesQueryKey;
