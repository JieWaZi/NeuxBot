import { readFile, readdir } from "node:fs/promises";
import { extname, relative } from "node:path";

const repositoryRoot = new URL("../", import.meta.url);
const sourceRoots = [
  "../packages/frontend/src/",
  "../web/src/",
  "../desktop/src/",
].map((path) => new URL(path, import.meta.url));
const supportedExtensions = new Set([".css", ".ts", ".tsx"]);

const rules = [
  {
    name: "任意字号",
    pattern: /\btext-\[[^\]]+\]/g,
    guidance:
      "使用 type-caption、type-ui、type-body、type-label、type-title、type-page-title 或 type-display。",
  },
  {
    name: "非语义字号",
    pattern: /\btext-(?:xs|sm|base|lg|xl|[2-9]xl)\b/g,
    guidance: "使用 NeuxBot 语义字号 Token，不直接使用 Tailwind 默认字号层级。",
  },
  {
    name: "任意行高",
    pattern:
      /\bleading-(?:\[[^\]]+\]|none|tight|snug|normal|relaxed|loose|[3-9]|10)\b/g,
    guidance: "行高由语义字号 Token 同时提供，不在组件中单独设置。",
  },
  {
    name: "任意圆角",
    pattern: /\brounded-\[[^\]]+\]/g,
    guidance:
      "使用 rounded-xs、rounded-sm、rounded-md、rounded-lg、rounded-xl、rounded-2xl、rounded-3xl、rounded-4xl 或 rounded-full。",
  },
  {
    name: "任意图标或头像尺寸",
    pattern: /\bsize-\[[^\]]+\]/g,
    guidance: "使用 size-icon-*、size-avatar-*、size-control-* 或 size-row-*。",
  },
  {
    name: "任意阴影",
    pattern: /\bshadow-\[[^\]]+\]/g,
    guidance:
      "使用 shadow-xs、shadow-sm、shadow-md、shadow-lg、shadow-composer 或 shadow-menu。",
  },
  {
    name: "越级阴影",
    pattern: /\bshadow-(?:xl|2xl)\b/g,
    guidance: "使用语义阴影层级；普通页面不得使用 xl 或 2xl 阴影。",
  },
  {
    name: "具体黑白颜色",
    pattern: /\b(?:bg|text|border|ring)-(?:white|black)(?:\/\d+)?\b/g,
    guidance: "使用 surface、foreground、primary、border 等主题语义颜色。",
  },
  {
    name: "具体颜色调色板",
    pattern:
      /\b(?:bg|text|border|ring)-(?:slate|gray|zinc|neutral|red|green|blue|emerald)-\d+\b/g,
    guidance: "将状态或图表颜色提升为浅色/暗色均有定义的语义 Token。",
  },
  {
    name: "非权威主题状态",
    pattern: /document\.documentElement\.classList\.contains\(["']dark["']\)/g,
    guidance:
      "React 组件通过 shared/theme 的 useTheme 获取 resolvedTheme；命令式第三方组件监听根节点 data-theme。",
  },
  {
    name: "非标准控件高度",
    pattern: /\b(?:h|min-h)-(?:7|8|9|10|11|12)\b/g,
    guidance: "交互控件使用 h-control-*，列表和导航使用 h-row-*。",
  },
  {
    name: "非标准动效时长",
    pattern: /\bduration-(?:75|100|150|200|300|500|700|1000)\b/g,
    guidance:
      "使用 duration-instant、duration-quick、duration-standard、duration-slow 或 duration-deliberate；进入和退出动效由共享组件提供。",
  },
  {
    name: "无边界过渡",
    pattern: /\btransition-all\b/g,
    guidance:
      "只声明实际变化的属性，避免 transition-all 扩散到布局和原生内容。",
  },
  {
    name: "零缩放动效",
    pattern: /\bscale-0\b|scale(?:3d)?\(\s*0(?:\s*[,)]|\.0+\s*[,)]?)/g,
    guidance: "Presence 和 Popup 使用透明度、轻微位移或不小于 0.985 的缩放。",
  },
  {
    name: "非标准字重",
    pattern: /\bfont-(?:thin|extralight|light|bold|extrabold|black)\b/g,
    guidance: "日常界面只使用 font-normal、font-medium 和 font-semibold。",
  },
];

async function collectFiles(directoryUrl) {
  const entries = await readdir(directoryUrl, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const childUrl = new URL(
      `${entry.name}${entry.isDirectory() ? "/" : ""}`,
      directoryUrl,
    );
    if (entry.isDirectory()) {
      if (entry.name === "generated") continue;
      files.push(...(await collectFiles(childUrl)));
      continue;
    }
    if (supportedExtensions.has(extname(entry.name))) files.push(childUrl);
  }
  return files;
}

function lineForOffset(source, offset) {
  return source.slice(0, offset).split("\n").length;
}

function excerptForOffset(source, offset) {
  const lineStart = source.lastIndexOf("\n", offset) + 1;
  const lineEnd = source.indexOf("\n", offset);
  return source
    .slice(lineStart, lineEnd === -1 ? source.length : lineEnd)
    .trim();
}

const avatarStudioPrefix =
  "packages/frontend/src/modules/waker/components/avatar-studio/";

const violations = [];
for (const fileUrl of (
  await Promise.all(sourceRoots.map(collectFiles))
).flat()) {
  const source = await readFile(fileUrl, "utf8");
  const file = relative(repositoryRoot.pathname, fileUrl.pathname);
  const isAvatarStudioAsset = file.startsWith(avatarStudioPrefix);

  if (isAvatarStudioAsset) continue;

  if (file === "packages/frontend/src/shared/styles/index.css") {
    const rootRule = source.match(/html,\s*body,\s*#root\s*\{([^}]*)\}/)?.[1];
    const rootFollowsViewport =
      rootRule?.includes("width: 100%;") &&
      rootRule.includes("min-width: 0;") &&
      rootRule.includes("min-height: 0;");
    if (!rootFollowsViewport) {
      violations.push({
        file,
        line: rootRule ? lineForOffset(source, source.indexOf(rootRule)) : 1,
        excerpt: rootRule?.trim() ?? "缺少 html、body、#root 共享规则",
        rule: {
          name: "根容器响应式边界",
          guidance:
            "DOM 根容器必须填满并服从实际窗口尺寸，保持 width: 100%、min-width: 0 和 min-height: 0。",
        },
        value: "根容器没有服从实际窗口尺寸",
      });
    }
  }

  for (const rule of rules) {
    for (const match of source.matchAll(rule.pattern)) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule,
        value: match[0],
      });
    }
  }

  const isTestFile = /\.(?:test|spec)\.(?:ts|tsx)$/.test(file);
  const isBusinessSource =
    /^desktop\/src\/(?:app|modules)\//.test(file) && !isTestFile;
  if (isBusinessSource) {
    for (const match of source.matchAll(/<input\b[^>]*>/gs)) {
      if (/\btype\s*=\s*["']file["']/.test(match[0])) continue;
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "未复用 Input",
          guidance:
            "普通输入框使用 shared/ui/input；原生 input 仅保留隐藏的文件选择控件。",
        },
        value: "原生 input",
      });
    }

    for (const match of source.matchAll(/<select\b[^>]*>/gs)) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "未复用 Select",
          guidance: "选择控件统一使用 shared/ui/select。",
        },
        value: "原生 select",
      });
    }

    for (const match of source.matchAll(
      /from\s+["']@radix-ui\/react-(?:checkbox|collapsible|dialog|dropdown-menu|popover|select|switch)["']/g,
    )) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "绕过共享基础组件",
          guidance:
            "业务层使用 shared/ui 中的基础组件，不直接引用已封装的 Radix 原语。",
        },
        value: match[0],
      });
    }

    for (const match of source.matchAll(
      /<(Button|Input|SearchField|SelectTrigger|Textarea)\b[^>]*>/gs,
    )) {
      const componentName = match[1];
      const literalClassName = match[0].match(
        /className=["']([^"']*)["']/,
      )?.[1];
      const sharedControlOverride = literalClassName?.match(
        /\b(?:h-control-[\w-]+|rounded-[\w-]+|shadow-[\w-]+|type-[\w-]+)\b/,
      );
      const buttonGeometryOverride =
        componentName === "Button"
          ? literalClassName?.match(
              /\b(?:h-|min-h-|p[trblxy]?-|gap-)[\w[\]./%()-]+/,
            )
          : undefined;
      const forbiddenOverride = sharedControlOverride ?? buttonGeometryOverride;
      if (!forbiddenOverride) continue;
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "覆盖共享控件核心样式",
          guidance:
            "通过共享组件的 size、shape、variant 等属性选择样式；className 只负责页面布局。",
        },
        value: forbiddenOverride[0],
      });
    }

    for (const match of source.matchAll(/<SearchField\b[^>]*>/gs)) {
      const literalContainerClassName = match[0].match(
        /containerClassName=["']([^"']*)["']/,
      )?.[1];
      const widthOverride = literalContainerClassName?.match(
        /\b(?:w|min-w|max-w)-(?!0\b)[\w[\]./%()-]+/,
      );
      if (!widthOverride) continue;
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "重复搜索框宽度",
          guidance:
            "列表工具栏使用 ResourcePageSearchField；其他搜索框通过 SearchField 的 width 变体或父布局分配宽度。",
        },
        value: widthOverride[0],
      });
    }

    for (const match of source.matchAll(
      /<(PopoverContent|SelectContent)\b[^>]*>/gs,
    )) {
      const literalClassName = match[0].match(
        /className=["']([^"']*)["']/,
      )?.[1];
      const widthOverride = literalClassName?.match(
        /\b(?:w|min-w|max-w)-(?!0\b)[\w[\]./%()-]+/,
      );
      if (!widthOverride) continue;
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "重复选择浮层宽度",
          guidance:
            "PopoverContent 使用共享 width 变体，SelectContent 保留共享触发框与视口宽度规则；业务层不覆盖浮层宽度。",
        },
        value: widthOverride[0],
      });
    }
  }

  const ownsThemeValues =
    file === "packages/frontend/src/shared/styles/theme.css" ||
    file === "packages/frontend/src/shared/styles/index.css";
  if (!isTestFile && !ownsThemeValues) {
    for (const match of source.matchAll(
      /#[0-9a-f]{3,8}\b|\b(?:rgb|hsl|oklab|oklch)a?\(/gi,
    )) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "业务层硬编码颜色",
          guidance:
            "将颜色提升到 shared/styles/theme.css 的语义 Token，再由组件使用对应主题变量。",
        },
        value: match[0],
      });
    }
  }

  const ownsMotionValues =
    file === "packages/frontend/src/shared/styles/motion.css" ||
    file === "packages/frontend/src/shared/styles/index.css";
  if (!isTestFile && !ownsMotionValues) {
    for (const match of source.matchAll(/animation\s*:[^;\n]*\binfinite\b/gi)) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "未授权无限动画",
          guidance:
            "无限动画只能由共享动效层为明确的加载或运行状态提供，并必须支持 reduced-motion。",
        },
        value: match[0],
      });
    }
  }

  if (
    file === "packages/frontend/src/modules/chat/components/chat-thread.tsx"
  ) {
    for (const match of source.matchAll(
      /\banimate-(?:fade|dialog|popover|collapse|shell)[\w-]*\b/g,
    )) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "流式消息挂载动画",
          guidance:
            "消息正文、推理和工具过程会高频增量更新，只允许加载/运行状态动画，不得挂载进入或退出动画。",
        },
        value: match[0],
      });
    }
  }

  const isOrdinaryModulePage =
    /^desktop\/src\/modules\/[^/]+\/pages\/[^/]+\.tsx$/.test(file) &&
    !file.endsWith("/chat-page.tsx");
  if (isOrdinaryModulePage) {
    for (const match of source.matchAll(/className="([^"]*)"/g)) {
      const classNames = match[1];
      if (
        classNames.includes("h-full") &&
        classNames.includes("overflow-y-auto") &&
        !classNames.includes("pt-shell-content-top")
      ) {
        violations.push({
          file,
          line: lineForOffset(source, match.index),
          excerpt: excerptForOffset(source, match.index),
          rule: {
            name: "Shell 内容顶部间距",
            guidance:
              "普通 Shell 页面主滚动容器统一使用 pt-shell-content-top；聊天消息流等结构性例外需在检查规则中明确说明。",
          },
          value: "缺少 pt-shell-content-top",
        });
      }
    }
  }

  const resourcePages =
    /desktop\/src\/modules\/(?:waker\/pages\/waker-list-page|team\/pages\/team-list-page|project\/pages\/project-list-page|memory\/pages\/waker-memory-page|skill\/pages\/waker-skills-page|connector\/pages\/waker-connectors-page)\.tsx$/;
  const requiredComponents = resourcePages.test(file)
    ? [
        "ResourcePageSurface",
        "ResourcePageHeader",
        "ResourcePageToolbar",
        "ResourcePageSearchField",
        "PageAction",
      ]
    : file === "packages/frontend/src/modules/waker/pages/waker-detail-page.tsx"
      ? ["PageAction"]
      : [];
  const resourceDetailPages =
    /desktop\/src\/modules\/(?:project\/pages\/project-detail-page|skill\/pages\/skill-detail-page|connector\/pages\/connector-detail-page)\.tsx$/;
  if (resourceDetailPages.test(file)) requiredComponents.push("PageAction");
  if (
    resourcePages.test(file) &&
    !file.includes("/memory/") &&
    !file.includes("/waker/") &&
    !file.includes("/team/")
  ) {
    requiredComponents.push("ResourceSelection", "ResourceSelectionActions");
  }
  if (resourcePages.test(file) && !file.includes("/memory/")) {
    requiredComponents.push("ResourceCardGrid");
  }
  for (const component of requiredComponents) {
    if (source.includes(`<${component}`)) continue;
    violations.push({
      file,
      line: 1,
      excerpt: file,
      rule: {
        name: "伙伴功能页统一布局",
        guidance:
          "标题、工具栏与右上角操作使用 shared/ui/resource-page，保持跨页面尺寸和间距一致。",
      },
      value: `缺少 ${component}`,
    });
  }

  if (resourcePages.test(file)) {
    for (const match of source.matchAll(/\b(?:sm|md|lg|xl|2xl):grid-cols-/g)) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "资源页使用视口列断点",
          guidance:
            "资源卡片使用 ResourceCardGrid，按 Scene 内容容器的实际宽度自动降列。",
        },
        value: match[0],
      });
    }
  }

  const isModuleDialog = /^desktop\/src\/modules\/[^/]+\/.*dialog\.tsx$/.test(
    file,
  );
  if (isModuleDialog) {
    for (const match of source.matchAll(/<footer\b/g)) {
      violations.push({
        file,
        line: lineForOffset(source, match.index),
        excerpt: excerptForOffset(source, match.index),
        rule: {
          name: "Dialog 操作区",
          guidance:
            "模块弹窗统一使用 shared/ui/dialog 的 DialogFooter，确保操作区固定在底部并右对齐。",
        },
        value: "原生 footer",
      });
    }
  }

  for (const match of source.matchAll(/strokeWidth=\{([^}]+)\}/g)) {
    if (match[1].trim() === "1.5") continue;
    violations.push({
      file,
      line: lineForOffset(source, match.index),
      excerpt: excerptForOffset(source, match.index),
      rule: {
        name: "非标准 Lucide 描边",
        guidance: "Lucide 图标统一使用 strokeWidth={1.5}。",
      },
      value: match[0],
    });
  }

  for (const match of source.matchAll(/\btracking-\[([^\]]+)\]/g)) {
    if (match[1] === "-0.01em") continue;
    violations.push({
      file,
      line: lineForOffset(source, match.index),
      excerpt: excerptForOffset(source, match.index),
      rule: {
        name: "任意字距",
        guidance: "正文不调整字距；页面大标题只允许 tracking-[-0.01em]。",
      },
      value: match[0],
    });
  }
}

if (violations.length > 0) {
  console.error(`UI 规范检查失败，共 ${violations.length} 项：\n`);
  for (const violation of violations) {
    console.error(
      `${violation.file}:${violation.line} [${violation.rule.name}] ${violation.value}`,
    );
    console.error(`  ${violation.excerpt}`);
    console.error(`  修复：${violation.rule.guidance}\n`);
  }
  process.exitCode = 1;
} else {
  console.log("UI 规范检查通过。");
}
