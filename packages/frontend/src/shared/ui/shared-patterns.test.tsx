import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CircleCheck, Settings } from "lucide-react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { ActivityItem } from "./activity-item";
import { Button } from "./button";
import {
  ComposerSendButton,
  ComposerStopButton,
  ComposerSurface,
} from "./composer";
import { DateRangePicker } from "./date-range-picker";
import { EmptyState } from "./empty-state";
import { Input } from "./input";
import {
  NavigationItem,
  NavigationPanel,
  NavigationSection,
} from "./navigation-panel";
import { PageHeader } from "./page-header";
import { ResourceFilterSelect } from "./resource-filter-select";
import { SceneTabStrip } from "./scene-tab-strip";
import { SceneChromeActionsProvider } from "./scene-chrome-actions";
import { SearchField } from "./search-field";
import { Switch } from "./switch";
import { Toolbar } from "./toolbar";
import {
  PageAction,
  PageActionLabel,
  ResourceCardGrid,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "./resource-page";

describe("共享界面模式", () => {
  it("资源工具栏使用唯一行高并保留控件与分隔线的间距", () => {
    render(
      <ResourcePageToolbar
        filters={<ResourcePageSearchField aria-label="搜索资源" />}
      >
        <span>全部技能</span>
      </ResourcePageToolbar>,
    );

    const leading = screen.getByText("全部技能").parentElement;
    const row = leading?.parentElement;
    const toolbar = row?.parentElement;
    for (const element of [leading, row]) {
      expect(element).toHaveClass("min-h-control-md");
      expect(element).not.toHaveClass("min-h-control-sm");
    }
    expect(toolbar).toHaveClass("pb-2", "border-b");
    expect(toolbar).not.toHaveClass("pb-3");
    expect(toolbar).not.toHaveClass("pb-0");
  });

  it("PageAction 在窄 PageHeader 下折叠标签并保留 aria-label", () => {
    render(
      <div style={{ width: 320 }}>
        <PageHeader
          title="记忆"
          actions={
            <PageAction aria-label="记忆设置">
              <Settings />
              <PageActionLabel>记忆设置</PageActionLabel>
            </PageAction>
          }
        />
      </div>,
    );

    expect(screen.getByRole("button", { name: "记忆设置" })).toHaveClass(
      "px-2",
    );
    expect(screen.getByText("记忆设置")).toHaveClass(
      "hidden",
      "@[32rem]/page-header:inline",
    );
  });

  it("detail 布局带 visual 时让 meta 行占满内容列宽度", () => {
    render(
      <PageHeader
        layout="detail"
        title="阿斯顿"
        visual={<span data-testid="page-header-visual">头像</span>}
        actions={<Button>发起对话</Button>}
      >
        <div
          className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1"
          data-testid="page-header-meta"
        >
          <span>2 个连接器 · 0 个技能</span>
          <span className="sm:ml-auto sm:text-right">2026年9月15日加入</span>
        </div>
      </PageHeader>,
    );

    const header = screen.getByRole("banner");
    expect(header).toHaveClass("flex", "flex-wrap");
    expect(screen.getByTestId("page-header-visual")).toBeVisible();
    expect(screen.getByTestId("page-header-meta").parentElement).toHaveClass(
      "min-w-0",
    );
    expect(screen.getByText("发起对话").parentElement).toHaveClass("shrink-0");
  });

  it("detail 布局在长简介下允许操作组整体换行", () => {
    render(
      <div style={{ width: 640 }}>
        <PageHeader
          layout="detail"
          title="私人助理"
          description="AI私人助理，协助你解答问题、整理资料、创作内容和制定计划，并在已配置的工具与授权范围内处理任务。"
          visual={<span data-testid="page-header-visual">头像</span>}
          actions={
            <>
              <Button variant="secondary">编辑</Button>
              <Button>会话</Button>
            </>
          }
        />
      </div>,
    );

    const editButton = screen.getByRole("button", { name: "编辑" });
    const chatButton = screen.getByRole("button", { name: "会话" });
    const actionsRow = editButton.parentElement;

    expect(actionsRow).toHaveClass("shrink-0");
    expect(actionsRow?.parentElement).toHaveClass("flex-wrap");
    expect(editButton.parentElement).toBe(actionsRow);
    expect(chatButton.parentElement).toBe(actionsRow);
  });

  it("统一页面标题和工具栏的结构职责", () => {
    render(
      <>
        <PageHeader
          title="项目"
          description="管理当前伙伴的项目"
          actions={<Button>新建</Button>}
        />
        <Toolbar
          data-testid="resource-toolbar"
          leading="共 2 项"
          trailing="搜索"
        />
      </>,
    );

    expect(screen.getByRole("heading", { name: "项目" })).toHaveClass(
      "type-page-title",
    );
    expect(screen.getByText("新建")).toBeVisible();
    expect(screen.getByText("新建").parentElement).toHaveClass(
      "max-w-full",
      "flex-wrap",
    );
    expect(screen.getByText("共 2 项")).toHaveClass("min-h-control-sm");
    const toolbar = screen.getByTestId("resource-toolbar");
    expect(toolbar.firstElementChild).toHaveClass(
      "flex",
      "min-w-0",
      "flex-wrap",
    );
    expect(toolbar.firstElementChild?.lastElementChild).toHaveClass(
      "flex-wrap",
      "flex-1",
      "min-w-0",
      "ml-auto",
      "max-w-full",
      "justify-end",
    );
    expect(toolbar.firstElementChild?.lastElementChild).not.toHaveClass(
      "flex-nowrap",
    );
    expect(toolbar.firstElementChild).not.toHaveClass("grid-cols-1");
  });

  it("资源页统一操作、搜索、页面内边距和自适应网格", () => {
    render(
      <ResourcePageSurface>
        <PageAction>新建项目</PageAction>
        <ResourcePageSearchField aria-label="搜索项目" />
        <ResourceFilterSelect
          ariaLabel="状态筛选"
          onValueChange={() => undefined}
          options={[
            { label: "全部状态", value: "all" },
            { label: "启用", value: "enabled" },
          ]}
          value="all"
        />
        <DateRangePicker
          from=""
          labels={{
            clear: "清除",
            empty: "全部时间",
            help: "选择记忆日期范围",
            title: "日期范围",
            trigger: "筛选记忆日期",
          }}
          onChange={() => undefined}
          to=""
        />
        <ResourceCardGrid data-testid="resource-grid">
          <div>项目一</div>
          <div>项目二</div>
        </ResourceCardGrid>
      </ResourcePageSurface>,
    );

    expect(screen.getByRole("button", { name: "新建项目" })).toHaveClass(
      "h-button-md",
      "type-ui",
      "gap-2",
      "px-2",
      "@[32rem]/page-header:px-3",
    );
    const resourceSearch = screen.getByRole("searchbox", {
      name: "搜索项目",
    }).parentElement;
    expect(resourceSearch).toHaveClass(
      "basis-field-search-min",
      "grow-[4]",
      "min-w-field-search-min",
      "max-w-field-search",
    );
    expect(resourceSearch).not.toHaveClass("max-w-full");
    expect(screen.getByRole("combobox", { name: "状态筛选" })).toHaveClass(
      "basis-field-filter-min",
      "grow",
      "min-w-field-filter-min",
      "max-w-field-filter",
    );
    expect(screen.getByRole("combobox", { name: "状态筛选" })).not.toHaveClass(
      "flex-none",
      "w-full",
      "w-field-filter",
    );
    expect(screen.getByRole("button", { name: "筛选记忆日期" })).toHaveClass(
      "w-auto",
      "max-w-field-date-range",
      "shrink-0",
    );
    expect(
      screen.getByRole("button", { name: "筛选记忆日期" }),
    ).not.toHaveClass("grow-[2]", "min-w-field-date-range-min");
    expect(screen.getByTestId("resource-grid")).toHaveClass(
      "grid-cols-1",
      "@[36rem]/resource-page:grid-cols-2",
      "@[56rem]/resource-page:grid-cols-3",
      "gap-3",
    );
  });

  it("统一空状态和复合输入外壳的视觉槽位", () => {
    render(
      <>
        <EmptyState
          icon={CircleCheck}
          title="暂无记录"
          description="完成后会显示在这里"
          actions={<Button>开始</Button>}
        />
        <ComposerSurface
          context="当前项目"
          leadingActions="附件"
          trailingActions="发送"
        >
          输入区域
        </ComposerSurface>
      </>,
    );

    expect(screen.getByText("暂无记录")).toBeVisible();
    expect(
      screen.getByText("当前项目").closest("[data-has-context]"),
    ).toHaveClass("bg-composer-context");
    expect(screen.getByText("发送")).toBeVisible();
  });

  it("导航项和活动项提供稳定的选中与展开语义", async () => {
    const user = userEvent.setup();
    function ActivityHarness() {
      const [expanded, setExpanded] = useState(false);
      return (
        <ActivityItem
          title="执行检查"
          description="等待结果"
          icon={<CircleCheck />}
          expanded={expanded}
          onExpandedChange={setExpanded}
        >
          检查详情
        </ActivityItem>
      );
    }

    render(
      <>
        <NavigationPanel aria-label="导航">
          <NavigationSection title="工作">
            <NavigationItem active icon={<Settings />} label="设置" />
          </NavigationSection>
        </NavigationPanel>
        <ActivityHarness />
      </>,
    );

    expect(screen.getByRole("button", { name: "设置" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await user.click(screen.getByRole("button", { name: /执行检查/ }));
    expect(screen.getByText("检查详情")).toBeVisible();
  });

  it("Scene Tab 统一选择、关闭和排序入口", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const onClose = vi.fn();
    render(
      <SceneTabStrip
        activeTabId="chat"
        ariaLabel="场景"
        onClose={onClose}
        onSelect={onSelect}
        tabs={[
          { id: "chat", label: "对话" },
          { id: "file", label: "文件", closeable: true },
        ]}
      />,
    );

    await user.click(screen.getByRole("tab", { name: "文件" }));
    await user.click(screen.getByRole("button", { name: "关闭 文件" }));
    expect(onSelect).toHaveBeenCalledWith("file");
    expect(onClose).toHaveBeenCalledWith("file");
  });

  it("Scene Tab 支持方向键导航和溢出菜单", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <SceneTabStrip
        activeTabId="chat"
        ariaLabel="场景"
        onClose={() => undefined}
        onSelect={onSelect}
        tabs={[
          { id: "chat", label: "对话" },
          { id: "file", label: "文件" },
          { id: "browser", label: "浏览器" },
        ]}
      />,
    );

    const tabList = screen.getByRole("tablist", { name: "场景" });
    Object.defineProperties(tabList, {
      clientWidth: { configurable: true, value: 180 },
      scrollWidth: { configurable: true, value: 420 },
    });
    fireEvent.resize(window);

    const chatTab = screen.getByRole("tab", { name: "对话" });
    chatTab.focus();
    await user.keyboard("{ArrowRight}");
    expect(onSelect).toHaveBeenCalledWith("file");
    expect(screen.getByRole("tab", { name: "文件" })).toHaveFocus();

    await user.click(screen.getByRole("button", { name: "场景中的其他标签" }));
    await user.click(screen.getByRole("menuitemradio", { name: "浏览器" }));
    expect(onSelect).toHaveBeenCalledWith("browser");
  });

  it("Scene Tab 溢出时仍保留末尾附加操作", () => {
    render(
      <SceneTabStrip
        activeTabId="chat"
        ariaLabel="场景"
        onClose={() => undefined}
        onSelect={() => undefined}
        tabs={[
          { id: "chat", label: "对话" },
          { id: "file", label: "文件" },
          { id: "browser", label: "浏览器" },
        ]}
        trailing={
          <button aria-label="添加内容" type="button">
            +
          </button>
        }
      />,
    );

    const tabList = screen.getByRole("tablist", { name: "场景" });
    Object.defineProperties(tabList, {
      clientWidth: { configurable: true, value: 180 },
      scrollWidth: { configurable: true, value: 420 },
    });
    fireEvent.resize(window);

    expect(screen.getByRole("button", { name: "添加内容" })).toBeVisible();
    expect(
      screen.getByRole("button", { name: "场景中的其他标签" }),
    ).toBeVisible();
    expect(
      document.querySelector("[data-scene-tab-trailing]"),
    ).not.toContainElement(tabList);
  });

  it("Scene 顶栏在 Tab 右侧承载场景操作", () => {
    render(
      <SceneChromeActionsProvider actions={<Button>新对话</Button>}>
        <SceneTabStrip
          activeTabId="chat"
          ariaLabel="场景"
          onClose={() => undefined}
          onSelect={() => undefined}
          tabs={[{ id: "chat", label: "对话" }]}
        />
      </SceneChromeActionsProvider>,
    );

    expect(screen.getByRole("tab", { name: "对话" })).toBeVisible();
    expect(screen.getByRole("button", { name: "新对话" })).toBeVisible();
  });

  it("共享操作控件使用统一几何和快速动效", () => {
    render(
      <>
        <Button>操作</Button>
        <Switch aria-label="功能开关" checked />
        <SceneTabStrip
          activeTabId="chat"
          ariaLabel="场景"
          onClose={() => undefined}
          onSelect={() => undefined}
          tabs={[{ id: "chat", label: "会话" }]}
        />
      </>,
    );

    expect(screen.getByRole("button", { name: "操作" })).toHaveClass(
      "rounded-control",
      "h-button-md",
      "type-ui",
      "px-4",
      "duration-quick",
    );
    const sceneTab = screen.getByRole("tab", { name: "会话" });
    expect(sceneTab.parentElement).toHaveClass(
      "rounded-control",
      "duration-quick",
    );
    expect(sceneTab).toHaveClass("rounded-control");
    const control = screen.getByRole("switch", { name: "功能开关" });
    expect(control).toHaveClass("h-4", "w-7", "duration-quick");
    expect(control.firstElementChild).toHaveClass("size-3", "duration-quick");
  });

  it("特殊启动器的几何由 Button 变体统一提供", () => {
    render(
      <Button size="icon-lg" variant="launcher">
        选择伙伴
      </Button>,
    );

    expect(screen.getByRole("button", { name: "选择伙伴" })).toHaveClass(
      "justify-start",
      "gap-0",
      "overflow-hidden",
      "p-0",
    );
  });

  it("图标按钮使用紧凑方形圆角", () => {
    render(
      <Button aria-label="设置" size="icon-sm" variant="ghost">
        <Settings />
      </Button>,
    );

    expect(screen.getByRole("button", { name: "设置" })).toHaveClass(
      "size-control-sm",
      "rounded-sm",
    );
  });

  it("Composer 发送按钮使用 28px 圆形 primary 样式", () => {
    render(<ComposerSendButton aria-label="发送" />);

    expect(screen.getByRole("button", { name: "发送" })).toHaveClass(
      "size-control-sm",
      "rounded-full",
      "bg-button-primary",
    );
  });

  it("Composer 停止按钮使用圆底方形指示", () => {
    render(<ComposerStopButton aria-label="停止" />);

    expect(screen.getByRole("button", { name: "停止" })).toHaveAttribute(
      "data-chat-composer-stop",
    );
    expect(
      screen
        .getByRole("button", { name: "停止" })
        .querySelector("[data-chat-composer-stop-indicator]"),
    ).toBeTruthy();
  });

  it("输入与搜索默认使用紧凑尺寸且不绘制额外聚焦光环", () => {
    render(
      <>
        <Input aria-label="名称" />
        <SearchField aria-label="搜索" />
      </>,
    );

    expect(screen.getByRole("textbox", { name: "名称" })).toHaveClass(
      "h-control-sm",
      "rounded-md",
    );
    expect(screen.getByRole("textbox", { name: "名称" })).not.toHaveClass(
      "focus:ring-2",
    );
    const search = screen.getByRole("searchbox", { name: "搜索" });
    expect(search.parentElement).toHaveClass(
      "h-control-search-sm",
      "rounded-md",
    );
    expect(search.parentElement).not.toHaveClass("focus-within:ring-2");
  });
});
