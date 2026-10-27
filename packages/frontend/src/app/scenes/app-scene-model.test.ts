import { describe, expect, it } from "vitest";

import {
  appSceneReducer,
  initialAppSceneSnapshot,
} from "@/shared/workspace/app-scene-model";
import {
  routeSceneForLocation as resolveRouteScene,
  settingsSceneTab as createSettingsSceneTab,
  type RouteSceneLocation,
  type RouteSceneLabels,
} from "./app-route-scene-model";

const labels: RouteSceneLabels = {
  resources: "资源库",
  chat: "会话",
  detail: "详情",
  newChat: "新会话",
  page: "页面",
  partner: "伙伴",
  settings: "设置",
  team: "团队",
  teamChat: "团队会话",
};

const routeSceneForLocation = (location: RouteSceneLocation) =>
  resolveRouteScene(location, labels);
const settingsSceneTab = () => createSettingsSceneTab(labels.settings);

describe("应用 Scene 模型", () => {
  it("关闭其他保留右键目标，工作台目标同时保留所属会话", () => {
    const next = appSceneReducer(
      {
        activeTabId: "settings",
        tabs: [
          {
            id: "chat",
            kind: "route",
            label: "会话",
            closeable: true,
            lastUsed: 1,
          },
          {
            id: "file",
            kind: "workspace",
            ownerTabId: "chat",
            workspaceTabId: "file-1",
            label: "文件",
            closeable: true,
            lastUsed: 2,
          },
          {
            id: "settings",
            kind: "settings",
            label: "设置",
            closeable: true,
            lastUsed: 3,
          },
          {
            id: "other-file",
            kind: "workspace",
            ownerTabId: "chat",
            workspaceTabId: "file-2",
            label: "另一个文件",
            closeable: true,
            lastUsed: 4,
          },
        ],
      },
      { type: "close-others", tabId: "file" },
    );
    expect(next.tabs.map((tab) => tab.id)).toEqual(["chat", "file"]);
    expect(next.activeTabId).toBe("file");
  });

  it("工作流列表复用资源库标签，工作流编辑仍有独立标签", () => {
    const library = routeSceneForLocation({
      pathname: "/resources",
      search: "",
    });
    const workflows = routeSceneForLocation({
      pathname: "/resources",
      search: "?category=workflows",
    });
    const legacy = routeSceneForLocation({
      pathname: "/workflows",
      search: "?search=周会",
    });
    expect(workflows.id).toBe(library.id);
    expect(legacy.id).toBe(library.id);
    expect(workflows.label).toBe("资源库");
    expect(
      routeSceneForLocation({ pathname: "/workflows/flow", search: "" }).id,
    ).toBe("workflow:flow");
  });
  it("知识库详情独立标签，同库视图复用且不同连接互不覆盖", () => {
    const resolve = (pathname: string, search = "") =>
      resolveRouteScene(
        { pathname, search },
        { ...labels, knowledge: "知识库" },
      );
    const list = resolve("/knowledge");
    const first = resolve("/knowledge/research", "?title=研究资料");
    const wiki = resolve(
      "/knowledge/research",
      "?title=研究资料&view=wiki&page=one",
    );
    const graph = resolve("/knowledge/research", "?title=研究资料&view=graph");
    expect(first.id).not.toBe(list.id);
    expect(first.label).toBe("研究资料");
    expect(
      resolve("/knowledge", "?library=research&view=raw&title=研究资料").id,
    ).toBe(first.id);
    expect(wiki.id).toBe(first.id);
    expect(graph.mountKey).toBe(first.mountKey);
    const tabs = [
      first,
      wiki,
      graph,
      resolve("/knowledge/atlas", "?title=Atlas"),
      resolve("/knowledge/shared", "?backend=custom&connection=one"),
      resolve("/knowledge/shared", "?backend=custom&connection=two"),
    ];
    let snapshot = initialAppSceneSnapshot(list, 1);
    for (const tab of tabs)
      snapshot = appSceneReducer(snapshot, { type: "open", tab, now: 2 });
    expect(snapshot.tabs).toHaveLength(5);
    expect(snapshot.tabs.find((tab) => tab.id === list.id)?.href).toBe(
      "/knowledge",
    );
    expect(snapshot.tabs.find((tab) => tab.id === first.id)?.href).toContain(
      "view=graph",
    );
  });
  it("重新打开工作流时清理该流程遗留的独立运行标签", () => {
    const workflow = routeSceneForLocation({
      pathname: "/workflows/flow-1",
      search: "",
    });
    const original = initialAppSceneSnapshot(workflow, 1);
    const snapshot = appSceneReducer(
      {
        ...original,
        tabs: [
          ...original.tabs,
          {
            ...workflow,
            id: "workflow-run:run-1",
            href: "/workflows/flow-1/runs/run-1",
            lastUsed: 2,
          },
        ],
      },
      { type: "open", tab: workflow, now: 3 },
    );
    expect(snapshot.tabs).toHaveLength(1);
    expect(snapshot.tabs[0]?.mountKey).toBe(workflow.mountKey);
  });
  it("工作流、运行结果和优化沿用同一标签及挂载身份", () => {
    const workflow = routeSceneForLocation({
      pathname: "/workflows/flow-1",
      search: "",
    });
    let snapshot = initialAppSceneSnapshot(workflow, 1);
    for (const location of [
      { pathname: "/workflows/flow-1/runs/run-1", search: "?result=1" },
      {
        pathname: "/workflows/flow-1",
        search: "?tab=canvas&optimizeRun=run-1",
      },
    ]) {
      snapshot = appSceneReducer(snapshot, {
        type: "open",
        tab: routeSceneForLocation(location),
        now: 2,
      });
      expect(snapshot.tabs).toHaveLength(1);
      expect(snapshot.tabs[0]?.mountKey).toBe(workflow.mountKey);
    }
  });
  it("工作流首次发送后复用新建画布 Scene 和挂载身份", () => {
    const draft = routeSceneForLocation({
      pathname: "/workflows/new",
      search: "?draftId=meeting",
    });
    const saved = routeSceneForLocation({
      pathname: "/workflows/flow-1",
      search: "",
    });
    const snapshot = appSceneReducer(initialAppSceneSnapshot(draft, 1), {
      type: "open",
      tab: saved,
      now: 2,
    });
    expect(draft.id).toBe("workflow-draft:meeting");
    expect(snapshot.tabs).toHaveLength(1);
    expect(snapshot.tabs[0]?.id).toBe("workflow:flow-1");
    expect(snapshot.tabs[0]?.mountKey).toBe(draft.mountKey);
  });
  it("切换 Scene 只改变激活项，不移除已经打开的设置", () => {
    const chat = routeSceneForLocation({
      pathname: "/chats/chat-a",
      search: "?wakerId=waker-1",
    });
    let snapshot = initialAppSceneSnapshot(chat, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: settingsSceneTab(),
      now: 2,
    });
    snapshot = appSceneReducer(snapshot, {
      type: "select",
      tabId: chat.id,
      now: 3,
    });

    expect(snapshot.activeTabId).toBe(chat.id);
    expect(snapshot.tabs.map((tab) => tab.id)).toEqual([chat.id, "settings"]);
  });

  it("同一伙伴的资源页面复用固定标题的详情 Scene", () => {
    const memory = routeSceneForLocation({
      pathname: "/wakers/waker-1/memory",
      search: "",
    });
    const skills = routeSceneForLocation({
      pathname: "/wakers/waker-1/skills",
      search: "",
    });
    let snapshot = initialAppSceneSnapshot(memory, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: skills,
      now: 2,
    });

    expect(memory.id).toBe(skills.id);
    expect(snapshot.tabs.map((tab) => tab.label)).toEqual(["详情"]);
    expect(snapshot.tabs[0]?.href).toBe("/wakers/waker-1/skills");
    expect(snapshot.activeTabId).toBe(skills.id);
  });

  it("真实会话就绪后替换解析中的会话入口 Scene", () => {
    const entry = routeSceneForLocation({
      pathname: "/chat",
      search: "?wakerId=waker-1",
    });
    const chat = routeSceneForLocation({
      pathname: "/chats/chat-a",
      search: "?wakerId=waker-1",
    });
    let snapshot = initialAppSceneSnapshot(entry, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: chat,
      now: 2,
    });

    expect(snapshot.tabs.map((tab) => tab.id)).toEqual(["chat:chat-a"]);
    expect(snapshot.activeTabId).toBe("chat:chat-a");
  });

  it("真实会话就绪后替换当前伙伴的临时会话 Scene", () => {
    const draft = routeSceneForLocation({
      pathname: "/chat",
      search: "?draftId=draft-a&wakerId=waker-1",
    });
    const chat = routeSceneForLocation({
      pathname: "/chats/chat-a",
      search: "?wakerId=waker-1",
    });
    let snapshot = initialAppSceneSnapshot(draft, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: chat,
      now: 2,
    });

    expect(snapshot.tabs.map((tab) => tab.id)).toEqual(["chat:chat-a"]);
    expect(snapshot.activeTabId).toBe("chat:chat-a");
    expect(snapshot.tabs[0]?.mountKey).toBe(draft.mountKey);
  });

  it("团队首发创建 Thread 后沿用草稿 Scene，不另开加载页", () => {
    const draft = routeSceneForLocation({
      pathname: "/teams/team-1",
      search: "?draftId=draft-a",
    });
    const thread = routeSceneForLocation({
      pathname: "/teams/team-1/threads/thread-1",
      search: "",
    });
    let snapshot = initialAppSceneSnapshot(draft, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: thread,
      now: 2,
    });

    expect(draft.id).toBe("team-draft:team-1:draft-a");
    expect(snapshot.tabs.map((tab) => tab.id)).toEqual([
      "team:team-1:thread:thread-1",
    ]);
    expect(snapshot.activeTabId).toBe("team:team-1:thread:thread-1");
    expect(snapshot.tabs[0]?.mountKey).toBe(draft.mountKey);
    expect(snapshot.tabs[0]?.href).toBe("/teams/team-1/threads/thread-1");
  });

  it("其他团队的 Thread 不替换当前团队草稿 Scene", () => {
    const draft = routeSceneForLocation({
      pathname: "/teams/team-1",
      search: "?draftId=draft-a",
    });
    const other = routeSceneForLocation({
      pathname: "/teams/team-2/threads/thread-2",
      search: "",
    });
    const snapshot = appSceneReducer(initialAppSceneSnapshot(draft, 1), {
      type: "open",
      tab: other,
      now: 2,
    });

    expect(snapshot.tabs.map((tab) => tab.id)).toEqual([
      draft.id,
      "team:team-2:thread:thread-2",
    ]);
    expect(snapshot.tabs[0]?.mountKey).toBe(draft.mountKey);
    expect(snapshot.tabs[1]?.mountKey).toBe(other.mountKey);
  });

  it("重新选择遗留的会话入口时清理已存在的重复 Scene", () => {
    const entry = routeSceneForLocation({
      pathname: "/chat",
      search: "?wakerId=waker-1",
    });
    const chat = routeSceneForLocation({
      pathname: "/chats/chat-a",
      search: "?wakerId=waker-1",
    });
    const snapshot = appSceneReducer(
      {
        activeTabId: entry.id,
        tabs: [
          { ...entry, lastUsed: 1 },
          { ...chat, label: "真实会话", lastUsed: 2 },
        ],
      },
      { type: "open", tab: chat, now: 3 },
    );

    expect(snapshot.tabs.map((tab) => tab.id)).toEqual(["chat:chat-a"]);
    expect(snapshot.activeTabId).toBe("chat:chat-a");
  });

  it("路由未变化时仍可通过 open 从设置切到自动任务", () => {
    const automaticTasks = routeSceneForLocation({
      pathname: "/automatic-tasks",
      search: "",
    });
    let snapshot = initialAppSceneSnapshot(automaticTasks, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: settingsSceneTab(),
      now: 2,
    });
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: automaticTasks,
      now: 3,
    });

    expect(snapshot.activeTabId).toBe("automatic-tasks");
    expect(snapshot.tabs.map((tab) => tab.id)).toEqual([
      "automatic-tasks",
      "settings",
    ]);
  });

  it("关闭当前 Scene 后回到最近使用的保留 Scene", () => {
    const memory = routeSceneForLocation({
      pathname: "/wakers/waker-1/memory",
      search: "",
    });
    let snapshot = initialAppSceneSnapshot(memory, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "open",
      tab: settingsSceneTab(),
      now: 2,
    });
    snapshot = appSceneReducer(snapshot, { type: "close", tabId: "settings" });

    expect(snapshot.activeTabId).toBe(memory.id);
    expect(snapshot.tabs.map((tab) => tab.id)).toEqual([memory.id]);
  });

  it("把会话工作区内容合并到统一 Scene 列表并保留主会话", () => {
    const chat = routeSceneForLocation({
      pathname: "/chats/chat-a",
      search: "?wakerId=waker-1",
    });
    let snapshot = initialAppSceneSnapshot(chat, 1);
    snapshot = appSceneReducer(snapshot, {
      type: "sync-workspace",
      activeWorkspaceTabId: "terminal:chat-a",
      now: 2,
      ownerHref: chat.href,
      ownerTabId: chat.id,
      primaryWorkspaceTabId: "chat:chat-a",
      tabs: [
        { id: "chat:chat-a", label: "对话" },
        { id: "terminal:chat-a", label: "终端", closeable: true },
      ],
    });

    expect(snapshot.tabs.map((tab) => tab.label)).toEqual(["会话", "终端"]);
    expect(snapshot.activeTabId).toBe("workspace:chat:chat-a:terminal:chat-a");

    snapshot = appSceneReducer(snapshot, {
      type: "sync-workspace",
      activeWorkspaceTabId: "chat:chat-a",
      now: 3,
      ownerHref: chat.href,
      ownerTabId: chat.id,
      primaryWorkspaceTabId: "chat:chat-a",
      tabs: [{ id: "chat:chat-a", label: "对话" }],
    });

    expect(snapshot.tabs.map((tab) => tab.label)).toEqual(["会话"]);
    expect(snapshot.activeTabId).toBe(chat.id);
  });
});
