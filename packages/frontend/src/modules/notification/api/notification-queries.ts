import { queryOptions, infiniteQueryOptions } from "@tanstack/react-query";
import {
  getNotificationSnapshot,
  getNotification,
  listNotifications,
} from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  notificationSnapshotSchema,
  notificationItemSchema,
  notificationListSchema,
  newerNotificationSnapshot,
  type NotificationList,
} from "../model/notification";

const notificationQueryKeys = {
  snapshot: ["notifications", "snapshot"] as const,
  lists: ["notifications", "list"] as const,
  items: ["notifications", "item"] as const,
};

/** 一份本地 Core 通知缓存跨地址重启保留，epoch 决定事实归属。 */
export function getNotificationSnapshotQueryKey() {
  return notificationQueryKeys.snapshot;
}
export function listNotificationsQueryKey(bucket?: "action" | "unread") {
  return bucket
    ? ([...notificationQueryKeys.lists, bucket] as const)
    : (["notifications", "list"] as const);
}

export function notificationItemQueryKey(noticeId?: string) {
  return noticeId
    ? ([...notificationQueryKeys.items, noticeId] as const)
    : notificationQueryKeys.items;
}

/** 阅读凭据复用统一通知缓存，由唯一通知消费者按原身份更新与恢复。 */
export function notificationItemOptions(noticeId: string) {
  return queryOptions({
    queryKey: notificationItemQueryKey(noticeId),
    queryFn: async ({ signal }) =>
      notificationItemSchema.parse(
        (
          await getNotification(
            withLocalErrorSurface({
              path: { noticeId },
              signal,
              throwOnError: true,
            }),
          )
        ).data,
      ),
    retry: false,
  });
}
export function notificationSnapshotOptions() {
  return queryOptions({
    queryKey: getNotificationSnapshotQueryKey(),
    queryFn: async (context) => {
      const started = notificationSnapshotSchema.safeParse(
        context.client.getQueryData(context.queryKey),
      ).data;
      const next = notificationSnapshotSchema.parse(
        (
          await getNotificationSnapshot(
            withLocalErrorSurface({
              signal: context.signal,
              throwOnError: true,
            }),
          )
        ).data,
      );
      const current = notificationSnapshotSchema.safeParse(
        context.client.getQueryData(context.queryKey),
      ).data;
      if (current && current.epoch !== started?.epoch) return current;
      return next;
    },
    structuralSharing: (previous, next) =>
      newerNotificationSnapshot(
        notificationSnapshotSchema.safeParse(previous).data,
        notificationSnapshotSchema.parse(next),
      ),
    staleTime: Infinity,
  });
}

export function notificationListOptions(bucket: "action" | "unread") {
  return infiniteQueryOptions({
    queryKey: listNotificationsQueryKey(bucket),
    initialPageParam: "",
    getNextPageParam: (page: NotificationList) =>
      page.hasMore ? page.nextCursor : undefined,
    queryFn: async ({ signal, pageParam }) =>
      notificationListSchema.parse(
        (
          await listNotifications(
            withLocalErrorSurface({
              query: {
                bucket,
                pageNum: 1,
                pageSize: 50,
                cursor: pageParam || undefined,
              },
              signal,
              throwOnError: true,
            }),
          )
        ).data,
      ),
    staleTime: Infinity,
  });
}
