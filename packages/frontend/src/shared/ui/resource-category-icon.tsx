import {
  Blocks,
  ChartScatter,
  FileCodeCorner,
  LayoutPanelTop,
  Network,
  NotebookPen,
  RadioTower,
  SwatchBook,
  Telescope,
  Waypoints,
} from "lucide-react";

import { cn } from "@/shared/lib/cn";

const appearances = [
  {
    Icon: FileCodeCorner,
    color: "bg-resource-category-code",
    names: ["编程", "开发", "coding", "development"],
  },
  {
    Icon: SwatchBook,
    color: "bg-resource-category-design",
    names: ["设计", "design"],
  },
  {
    Icon: ChartScatter,
    color: "bg-resource-category-data",
    names: [
      "数据",
      "数据分析",
      "数据与分析",
      "data",
      "analytics",
      "data & analytics",
      "data-analytics",
    ],
  },
  {
    Icon: NotebookPen,
    color: "bg-resource-category-writing",
    names: ["写作", "文档", "writing", "documents"],
  },
  {
    Icon: Telescope,
    color: "bg-resource-category-research",
    names: ["研究", "调研", "research"],
  },
  {
    Icon: Network,
    color: "bg-resource-category-web",
    names: ["网络", "网页", "浏览器", "web", "browser"],
  },
  {
    Icon: Waypoints,
    color: "bg-resource-category-automation",
    names: ["自动化", "工作流", "automation", "workflow"],
  },
  {
    Icon: LayoutPanelTop,
    color: "bg-resource-category-business",
    names: ["办公", "商务", "business", "productivity"],
  },
  {
    Icon: RadioTower,
    color: "bg-resource-category-communication",
    names: ["沟通", "通讯", "communication"],
  },
  {
    Icon: Blocks,
    color: "bg-resource-category-tools",
    names: ["工具", "其他", "tools", "other"],
  },
] as const;

function categoryAppearance(category: string) {
  const key = category.normalize("NFKC").trim().toLowerCase();
  if (!key) return appearances[9];
  const preset = appearances.find(({ names }) =>
    names.some((name) => name === key),
  );
  if (preset) return preset;
  // 自设分类按名称稳定选槽，不依赖列表顺序、搜索结果或本地状态。
  let hash = 0;
  for (const character of key)
    hash = (Math.imul(hash, 31) + (character.codePointAt(0) ?? 0)) >>> 0;
  return appearances[hash % appearances.length];
}

/** 资源共用十组分类标识，未分类统一使用积木图标与同一底色。 */
export function ResourceCategoryIcon({
  category = "",
  size = "list",
  className,
}: {
  category?: string;
  size?: "list" | "detail";
  className?: string;
}) {
  const appearance = categoryAppearance(category) ?? appearances[9];
  const Icon = appearance.Icon;
  return (
    <span
      aria-hidden="true"
      className={cn(
        "grid shrink-0 place-items-center rounded-lg",
        size === "detail" ? "size-resource-detail-icon" : "size-resource-icon",
        appearance.color,
        "text-resource-category-content",
        className,
      )}
    >
      <Icon className="size-resource-logo" strokeWidth={1.5} />
    </span>
  );
}
