import { describe, expect, it } from "vitest";
import {
  recordEntityHistory,
  nearestEntityHistory,
  chatOwnerForScene,
  chatScenesForOwner,
} from "./entity-navigation-history";
import type { AppSceneTab } from "@/shared/workspace/app-scene-model";

const a = { kind: "waker", id: "a" } as const;
const b = { kind: "waker", id: "b" } as const;
const team = { kind: "team", id: "team" } as const;

describe("实体导航历史", () => {
  it("同身份重复聊天及覆盖页不追加，返回后新访问截断前进", () => {
    let history = recordEntityHistory({ entries: [], index: -1 }, a);
    expect(recordEntityHistory(history, a)).toBe(history);
    expect(recordEntityHistory(history, undefined)).toBe(history);
    history = recordEntityHistory(recordEntityHistory(history, b), team);
    history = recordEntityHistory(history, a, 0);
    expect(history.index).toBe(0);
    history = recordEntityHistory(history, team);
    expect(history).toEqual({ entries: [a, team], index: 1 });
  });
  it("后退前进跳过已失效实体", () => {
    const history = { entries: [a, b, team], index: 2 };
    expect(nearestEntityHistory(history, -1, (entry) => entry.id !== "b")).toBe(
      0,
    );
    expect(
      nearestEntityHistory(
        { ...history, index: 0 },
        1,
        (entry) => entry.id !== "b",
      ),
    ).toBe(2);
  });
});

describe("当前身份聊天投影", () => {
  const scenes: AppSceneTab[] = [
    {
      id: "chat:a",
      kind: "route",
      href: "/chats/a",
      label: "同名",
      lastUsed: 1,
      chatOwner: a,
    },
    {
      id: "chat:b",
      kind: "route",
      href: "/chats/b?wakerId=b",
      label: "同名",
      lastUsed: 2,
    },
    {
      id: "unknown",
      kind: "route",
      href: "/chats/unknown",
      label: "未就绪",
      lastUsed: 3,
    },
    {
      id: "draft",
      kind: "route",
      href: "/chat?wakerId=a&draftId=draft",
      label: "新会话",
      lastUsed: 4,
    },
    {
      id: "resource",
      kind: "route",
      href: "/wakers/a/memory",
      label: "资源",
      lastUsed: 5,
    },
    {
      id: "team",
      kind: "route",
      href: "/teams/team/threads/one",
      label: "同名",
      lastUsed: 6,
    },
    {
      id: "history",
      kind: "route",
      href: "/teams/team/chats",
      label: "历史",
      lastUsed: 7,
    },
    {
      id: "member",
      kind: "workspace",
      ownerTabId: "team",
      label: "成员",
      lastUsed: 8,
    },
  ];
  it("裸深链只用已解析 Query 元数据，未知 owner 不猜伙伴", () => {
    expect(chatOwnerForScene(scenes[0])).toEqual(a);
    expect(chatOwnerForScene(scenes[2])).toBeUndefined();
    expect(chatScenesForOwner(scenes, a).map((tab) => tab.id)).toEqual([
      "chat:a",
      "draft",
    ]);
  });
  it("团队下拉只含 Thread，成员与历史页不混入", () => {
    expect(chatScenesForOwner(scenes, team).map((tab) => tab.id)).toEqual([
      "team",
    ]);
  });
});
