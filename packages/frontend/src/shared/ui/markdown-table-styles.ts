/** 只读 Markdown 容器内 Streamdown 表格：在父级宽度内铺满列宽。 */
export const markdownTableContainerClassName =
  "[&_[data-streamdown=table-wrapper]]:w-full [&_[data-streamdown=table-wrapper]]:max-w-none [&_[data-streamdown=table-wrapper]>div]:min-w-0 [&_[data-streamdown=table-wrapper]>div]:w-full [&_[data-streamdown=table]]:min-w-full [&_[data-streamdown=table]]:w-full [&_[data-streamdown=table]]:table-fixed [&_[data-streamdown=table]_th]:w-[1%] [&_[data-streamdown=table]_th]:break-words [&_[data-streamdown=table]_th]:whitespace-normal [&_[data-streamdown=table]_td]:w-[1%] [&_[data-streamdown=table]_td]:break-words";

/** 直接挂在 `<table>` 上的铺满列宽样式。 */
export const markdownTableLayoutClassName =
  "min-w-full w-full table-fixed border-collapse [&_th]:w-[1%] [&_th]:break-words [&_th]:whitespace-normal [&_td]:w-[1%] [&_td]:break-words";
