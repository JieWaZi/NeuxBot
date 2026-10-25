import { defineCatalog, type Spec } from "@json-render/core";
import { schema } from "@json-render/react";
import { z } from "zod";

const value = z.unknown();
const text = z.union([z.string(), z.object({}).catchall(z.unknown())]);
const gap = z.enum(["none", "small", "medium", "large"]);
const align = z.enum(["start", "center", "end", "stretch"]);
const tone = z.enum([
  "default",
  "accent",
  "info",
  "success",
  "warning",
  "danger",
]);
const layout = {
  Stack: {
    props: z.object({
      direction: z.enum(["vertical", "horizontal"]).optional(),
      gap: z.enum(["small", "medium", "large"]).optional(),
    }),
    slots: ["default"],
    description: "按顺序排列子组件",
  },
  Grid: {
    props: z.object({ columns: z.number().int().min(1).max(4).optional() }),
    slots: ["default"],
    description: "响应式网格",
  },
  Section: {
    props: z.object({ title: text.optional(), description: text.optional() }),
    slots: ["default"],
    description: "带标题的内容分区",
  },
};
export const resultComponents = {
  Page: {
    props: z.object({
      width: z.enum(["readable", "wide", "full"]).optional(),
      align: z.enum(["start", "center"]).optional(),
      gap: gap.optional(),
    }),
    slots: ["default"],
    description:
      "完整结果页根容器。Agent 设计其全部子树；readable 适合长报告，wide 适合卡片与数据，full 适合宽表格。页面标题放入 Heading，避免重复标题。",
    example: { width: "wide", align: "center", gap: "large" },
  },
  Stack: {
    props: z.object({
      direction: z.enum(["vertical", "horizontal"]).optional(),
      gap: gap.optional(),
      align: align.optional(),
      justify: z.enum(["start", "center", "end", "between"]).optional(),
    }),
    slots: ["default"],
    description:
      "自由组合纵向内容或可换行的横向标题、状态与来源，选择间距、对齐和分布。",
    example: {
      direction: "horizontal",
      gap: "small",
      align: "center",
      justify: "between",
    },
  },
  Grid: {
    props: z.object({
      columns: z.number().int().min(1).max(4).optional(),
      gap: gap.optional(),
      ratio: z.enum(["equal", "wide-start", "wide-end"]).optional(),
      align: align.optional(),
    }),
    slots: ["default"],
    description:
      "按所在容器宽度降列的网格。双列可选择主次栏比例；窄聊天和右侧 Tab 自动单列。",
    example: { columns: 2, ratio: "wide-start", gap: "large", align: "start" },
  },
  Section: {
    props: z.object({
      title: text.optional(),
      description: text.optional(),
      gap: gap.optional(),
    }),
    slots: ["default"],
    description:
      "无重复底板的内容分区；标题、说明和内容建立阅读节奏。背景与强调由 Card 单独选择。",
  },
  Card: {
    props: z.object({ tone: tone.optional() }),
    slots: ["default"],
    description:
      "通用语义表面容器，可组织任意组件，不自带业务字段或固定布局。使用主题中性或强调底色，无常驻边框和阴影；避免层层套 Card。",
    example: { tone: "accent" },
  },
  Heading: {
    props: z.object({
      value,
      level: z.enum(["h1", "h2", "h3"]).optional(),
      align: z.enum(["start", "center", "end"]).optional(),
    }),
    description:
      "页面、分区、条目标题的真实排版层级；每页一个 h1，正文不要用标题样式。",
    example: { value: "本次分析", level: "h1" },
  },
  Callout: {
    props: z.object({ title: text.optional(), value, tone: tone.optional() }),
    description:
      "突出关键结论、风险或数据覆盖边界；tone 来自语义主题，不暗示未经确认的成功。",
    example: {
      title: "阅读范围",
      value: "建议基于已提供的信息。",
      tone: "info",
    },
  },
  Disclosure: {
    props: z.object({ title: text, defaultOpen: z.boolean().optional() }),
    slots: ["default"],
    description:
      "用户可展开的辅助证据与长细节，完整内容保留在页面中；核心结论和下一步不要默认折叠。",
  },
  Text: {
    props: z.object({
      value,
      tone: z.enum(["default", "muted", "accent"]).optional(),
      size: z.enum(["body", "label", "caption"]).optional(),
      weight: z.enum(["normal", "medium", "semibold"]).optional(),
      align: z.enum(["start", "center", "end"]).optional(),
    }),
    description: "普通文本；长文本可展开",
  },
  Markdown: {
    props: z.object({ value }),
    description: "安全 Markdown；长内容可展开，不加载远程图片",
  },
  KeyValue: {
    props: z.object({ label: text, value }),
    description: "名称和值",
  },
  Metric: {
    props: z.object({
      label: text,
      value,
      unit: text.optional(),
      description: text.optional(),
      tone: tone.optional(),
    }),
    description:
      "真实指标，强调数值与口径，辅助说明可解释覆盖范围；同组指标共享 Grid，不重复展示同义数量。",
  },
  Badge: {
    props: z.object({
      value,
      tone: z.enum(["default", "success", "warning", "danger"]).optional(),
    }),
    description: "状态标签",
  },
  Table: {
    props: z.object({
      rows: value,
      columns: z
        .array(z.object({ key: z.string(), label: z.string() }))
        .min(1)
        .max(20),
      pageSize: z.number().int().min(1).max(50).optional(),
    }),
    description: "分页数据表，key 是行内字段名",
  },
  Collection: {
    props: z.object({
      items: value,
      pageSize: z.number().int().min(1).max(50).optional(),
      layout: z.enum(["list", "grid"]).optional(),
      columns: z.number().int().min(1).max(4).optional(),
      gap: gap.optional(),
    }),
    slots: ["default"],
    description:
      "分页集合，可按列表或响应式卡片网格展示。Agent 设计每项完整子树，通过 $item 和 $index 读取当前项，无需 repeat。",
  },
  Link: {
    props: z.object({
      label: text,
      href: value,
      variant: z.enum(["inline", "button"]).optional(),
    }),
    description: "仅支持 http/https 的外部链接",
  },
  FileList: {
    props: z.object({ files: value, label: text.optional() }),
    description:
      "仅用于用户明确需要的真实交付文件；label 使用可读交付名称。内部 JSON 缓存、完整响应和中间数据不属于交付物，不应声明或展示。files 为 Schema 中明确声明的真实路径或路径数组。",
  },
};
export const inputComponents = {
  ...layout,
  Field: {
    props: z.object({ path: z.string().regex(/^\/args(?:\/.*)?$/) }),
    description:
      "绑定 inputSchema 字段的完整输入组件；类型、标题、校验和文件能力来自 Schema，path 是 JSON Pointer",
  },
  Text: {
    props: z.object({ value, tone: z.enum(["default", "muted"]).optional() }),
    description: "输入表单中的说明文本",
  },
};
export const resultCatalog = defineCatalog(schema, {
  components: resultComponents,
  actions: {},
});
export const inputCatalog = defineCatalog(schema, {
  components: inputComponents,
  actions: {},
});
export type PageDefinition = {
  formatVersion: 1;
  catalogVersion: "workflow-result-v1" | "workflow-input-v1";
  catalogDigest?: string;
  title?: string;
  description?: string;
  spec: Spec;
};
