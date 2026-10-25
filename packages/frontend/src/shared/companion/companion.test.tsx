import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import {
  createCompanion,
  companionReactions,
  type CompanionEngine,
} from "../../../../../vendor/companion/renderer";
import {
  companionAppearance,
  companionAvatar,
  randomCompanionAvatar,
} from "./appearance";
import { companionPlayback } from "./playback";
import { CompanionAvatar } from "./companion-avatar";
import { Avatar } from "@/shared/ui/avatar";
import {
  CompanionInteraction,
  CompanionInteractionProvider,
} from "@/shared/ui/companion-interaction";
import "@/shared/i18n";

const avatar = "companion:v1:0123456789abcdef0123456789abcdef";
function fakeEngine() {
  return {
    setEmotion: vi.fn(),
    setActive: vi.fn(),
    renderStatic: vi.fn(),
    destroy: vi.fn(),
    ball: {
      setReduced: vi.fn(),
      setResolution: vi.fn(),
      perform: vi.fn<CompanionEngine["ball"]["perform"]>(() => true),
      cancelAction: vi.fn(),
    },
  };
}
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("伙伴形象", () => {
  it("随机身份可持久化，改名不改变已保存的身体风格", () => {
    const value = randomCompanionAvatar();
    expect(value).toMatch(/^companion:v2:[0-9a-f]{32}$/);
    expect(randomCompanionAvatar()).not.toBe(value);
    expect(companionAvatar(value, "新名字")).toBe(value);
    expect(companionAppearance(value)).toEqual(
      companionAppearance(JSON.parse(JSON.stringify(value))),
    );
    expect(companionAppearance(value)).not.toHaveProperty("accessories");
  });
  it("多个实例渲染静态身体和表情，SVG 引用彼此独立", () => {
    const host = document.createElement("div");
    const options = {
      character: "ally" as const,
      emotion: "02",
      ...companionAppearance(avatar),
      autostart: false as const,
      lite: false as const,
      idle: false as const,
    };
    const first = createCompanion(host, options);
    const second = createCompanion(host, options);
    expect(host.querySelectorAll("svg")).toHaveLength(2);
    expect(host.querySelectorAll("image")).toHaveLength(2);
    expect(host.querySelector("image")?.getAttribute("href")).toContain(
      "-256.png",
    );
    expect(host.querySelector("feColorMatrix")).toBeNull();
    const ids = Array.from(host.querySelectorAll("[id]")).map(
      (node) => node.id,
    );
    expect(new Set(ids).size).toBe(ids.length);
    first.setEmotion("40");
    first.ball.setReduced(true);
    first.renderStatic();
    expect(host.querySelector(".ally-role-badge")).toBeNull();
    expect(
      host.querySelector('.ally-effects-front [data-effect="40"]'),
    ).toHaveAttribute("opacity", "1");
    first.setEmotion("32");
    first.renderStatic();
    expect(
      host
        .querySelector('.ally-effects-back [data-effect="32"] path')
        ?.getAttribute("d"),
    ).toContain("L");
    expect(
      host
        .querySelector('.ally-effects-front [data-effect="32"] path')
        ?.getAttribute("d"),
    ).toContain("L");
    for (const reaction of companionReactions) {
      first.ball.cancelAction();
      first.setEmotion(reaction.emotion);
      if (reaction.action)
        expect(first.ball.perform(reaction.action)).toBe(true);
      first.renderStatic();
      expect(host.querySelector(".ally-eye path")?.getAttribute("d")).toMatch(
        /^M/,
      );
      expect(host.innerHTML).not.toContain("NaN");
    }
    first.destroy();
    second.destroy();
    expect(host.childElementCount).toBe(0);
  });
  it("透明的非当前特效不再逐帧写入 DOM", () => {
    const host = document.createElement("div");
    const engine = createCompanion(host, {
      character: "ally",
      emotion: "30",
      ...companionAppearance(avatar),
      autostart: false,
      lite: false,
      idle: false,
    });
    engine.ball.setReduced(true);
    engine.renderStatic();
    const hiddenLayers = Array.from(
      host.querySelectorAll('[data-effect="40"]'),
    );
    const writes: Element[] = [];
    const original = Element.prototype.setAttribute;
    const spy = vi
      .spyOn(Element.prototype, "setAttribute")
      .mockImplementation(function (this: Element, name, value) {
        if (hiddenLayers.some((layer) => layer.contains(this)))
          writes.push(this);
        original.call(this, name, value);
      });
    engine.renderStatic();
    spy.mockRestore();
    engine.destroy();
    expect(writes).toEqual([]);
  });
  it("完成只在运行到完成时强调，后续任务会取消收尾计时器", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "completed", {
      reactions: companionReactions,
    });
    expect(engine.setEmotion).toHaveBeenLastCalledWith("02");
    playback.update("working");
    playback.update("completed");
    expect(engine.setEmotion).toHaveBeenLastCalledWith("33");
    playback.update("searching");
    vi.advanceTimersByTime(2500);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("40");
    playback.update("completed");
    vi.advanceTimersByTime(2000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("02");
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("空闲随机动作，90 秒休息，工作立即唤醒并打断动作", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
    });
    playback.visibility(true, false);
    vi.advanceTimersByTime(26_000);
    expect(engine.ball.perform).toHaveBeenCalled();
    vi.advanceTimersByTime(64_000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("00");
    playback.wake();
    expect(engine.setEmotion).toHaveBeenLastCalledWith("02");
    playback.update("working");
    vi.advanceTimersByTime(100_000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("32");
    expect(engine.ball.cancelAction).toHaveBeenCalled();
    playback.destroy();
  });
  it("离屏和减少动态均不触发随机动作", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
    });
    playback.visibility(false, false);
    vi.advanceTimersByTime(30_000);
    expect(engine.ball.perform).not.toHaveBeenCalled();
    playback.visibility(true, true);
    vi.advanceTimersByTime(60_000);
    expect(engine.ball.perform).not.toHaveBeenCalled();
    expect(engine.setEmotion).toHaveBeenLastCalledWith("00");
    expect(engine.setActive).toHaveBeenLastCalledWith(false);
    playback.destroy();
  });
  it("展示态空闲更频繁地做张望和小动作", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
      preview: true,
    });
    playback.visibility(true, false);
    vi.advanceTimersByTime(6000);
    expect(engine.setEmotion.mock.lastCall?.[0]).not.toBe("02");
    playback.destroy();
  });
  it("组件卸载会释放真实动画实例", async () => {
    const view = render(<CompanionAvatar avatar={avatar} />);
    await waitFor(() =>
      expect(view.container.querySelector("svg")).not.toBeNull(),
    );
    await act(async () =>
      view.rerender(<CompanionAvatar avatar={avatar} activity="searching" />),
    );
    expect(
      view.container.querySelector("[data-companion-activity]"),
    ).toHaveAttribute("data-companion-activity", "searching");
    view.unmount();
    expect(view.container.childElementCount).toBe(0);
  });
  it("漫游复用空闲时钟，互动、任务、隐藏和休息都停止位移", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const engine = fakeEngine();
    const motion = { move: vi.fn(() => true), stop: vi.fn() };
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
      preview: true,
      motion,
    });
    playback.visibility(true, false);
    vi.advanceTimersByTime(6000);
    const firstMove = motion.move.mock.lastCall;
    expect(engine.ball.perform).toHaveBeenCalled();
    vi.advanceTimersByTime(6000);
    expect(motion.move).toHaveBeenCalledTimes(1);
    expect(engine.setEmotion.mock.lastCall?.[0]).not.toBe("02");
    vi.advanceTimersByTime(6000);
    expect(motion.move).toHaveBeenCalledTimes(2);
    expect(motion.move.mock.lastCall).not.toEqual(firstMove);
    motion.stop.mockClear();
    playback.interact();
    expect(motion.stop).toHaveBeenCalled();
    playback.update("searching");
    motion.move.mockClear();
    vi.advanceTimersByTime(100_000);
    expect(motion.move).not.toHaveBeenCalled();
    playback.update("idle");
    playback.visibility(false, false);
    vi.advanceTimersByTime(20_000);
    expect(motion.move).not.toHaveBeenCalled();
    playback.visibility(true, true);
    vi.advanceTimersByTime(70_000);
    expect(motion.move).not.toHaveBeenCalled();
    expect(engine.setEmotion).toHaveBeenLastCalledWith("00");
    playback.wake();
    playback.visibility(true, false);
    vi.advanceTimersByTime(90_000);
    expect(motion.move).toHaveBeenCalled();
    expect(engine.setEmotion).toHaveBeenLastCalledWith("00");
    motion.move.mockClear();
    vi.advanceTimersByTime(60_000);
    expect(motion.move).not.toHaveBeenCalled();
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("自动表演穿插全部生活表情和动作，轻抖、好奇、开心均可自行出现", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
      preview: true,
      motion: { move: () => true, stop: () => {} },
    });
    playback.visibility(true, false);
    for (let round = 0; round < 6; round++) {
      vi.advanceTimersByTime(90_000);
      expect(engine.setEmotion).toHaveBeenLastCalledWith("00");
      playback.wake();
    }
    const emotions = engine.setEmotion.mock.calls.map(([emotion]) => emotion);
    expect(emotions).toEqual(
      expect.arrayContaining(["03", "07", "10", "11", "13", "19"]),
    );
    expect(engine.ball.perform.mock.calls.map(([action]) => action)).toEqual(
      expect.arrayContaining(["walk", "jump", "sway", "spin", "love", "party"]),
    );
    expect(emotions).not.toContain("32");
    expect(emotions).not.toContain("40");
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("点击唤醒并回应，工作中保留状态，新任务打断互动且释放计时器", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "idle", {
      reactions: companionReactions,
    });
    playback.visibility(true, false);
    vi.advanceTimersByTime(90_000);
    playback.interact();
    expect(
      companionReactions.some(
        (reaction) => reaction.emotion === engine.setEmotion.mock.lastCall?.[0],
      ),
    ).toBe(true);
    playback.update("working");
    playback.interact();
    expect(engine.setEmotion).toHaveBeenLastCalledWith("32");
    playback.update("searching");
    vi.advanceTimersByTime(3000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("40");
    playback.visibility(true, true);
    playback.interact();
    expect(engine.renderStatic).toHaveBeenCalled();
    expect(engine.setActive).toHaveBeenLastCalledWith(false);
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("空对话问候头像连点或上箭头不唤出输入区形象", () => {
    render(
      <CompanionInteractionProvider
        scope="draft"
        activeAvatar={avatar}
        activity="idle"
      >
        <Avatar name="伙伴" src={avatar} summon={false} />
        <CompanionInteraction />
      </CompanionInteractionProvider>,
    );
    const trigger = screen.getByRole("button", { name: "与 伙伴 互动" });
    expect(trigger).toHaveAttribute("title", "点一下，和我互动");
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    fireEvent.keyDown(trigger, { key: "ArrowUp" });
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });
  it("第二次点击触发卡片展示，第三次仍可唤出形象", () => {
    const onDoubleInteract = vi.fn();
    render(
      <CompanionInteractionProvider
        scope="chat-card"
        activeAvatar={avatar}
        activity="idle"
      >
        <Avatar name="伙伴" src={avatar} onDoubleInteract={onDoubleInteract} />
        <CompanionInteraction />
      </CompanionInteractionProvider>,
    );
    const trigger = screen.getByRole("button", { name: "与 伙伴 互动" });
    fireEvent.click(trigger);
    expect(onDoubleInteract).not.toHaveBeenCalled();
    fireEvent.click(trigger);
    expect(onDoubleInteract).toHaveBeenCalledTimes(1);
    fireEvent.click(trigger);
    expect(
      screen.getByRole("region", { name: "输入框上方的 伙伴" }),
    ).toBeInTheDocument();
  });
  it("三击在输入区唤出唯一形象，输入仍可用，收起与切换聊天释放形象", async () => {
    const parentClick = vi.fn();
    const fixture = (scope: string) => (
      <CompanionInteractionProvider
        scope={scope}
        activeAvatar={avatar}
        activity="searching"
      >
        <div onClick={parentClick}>
          <Avatar name="伙伴" src={avatar} />
        </div>
        <CompanionInteraction />
        <input aria-label="消息" />
      </CompanionInteractionProvider>
    );
    const view = render(fixture("chat-1"));
    const trigger = screen.getByRole("button", { name: "与 伙伴 互动" });
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    fireEvent.click(trigger);
    expect(parentClick).not.toHaveBeenCalled();
    expect(
      screen.getByRole("region", { name: "输入框上方的 伙伴" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.querySelector('[data-motion="overlay"]')).toBeNull();
    await waitFor(() =>
      expect(document.querySelectorAll(".ally-body-motion")).toHaveLength(2),
    );
    const touch = screen.getByRole("button", { name: "摸摸 伙伴" });
    expect(touch).toHaveClass("pointer-events-none");
    expect(touch.querySelector("svg")).toHaveAttribute(
      "pointer-events",
      "visiblePainted",
    );
    await userEvent.type(
      screen.getByRole("textbox", { name: "消息" }),
      "继续聊天",
    );
    expect(screen.getByRole("textbox")).toHaveValue("继续聊天");
    expect(screen.getByRole("textbox")).toHaveFocus();
    fireEvent.click(touch);
    for (let index = 0; index < 3; index++) fireEvent.click(trigger);
    expect(screen.getAllByRole("region")).toHaveLength(1);
    await userEvent.hover(touch);
    touch.focus();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "收起形象" })).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "收起形象" }));
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(document.querySelectorAll(".ally-body-motion")).toHaveLength(1);
    expect(trigger).toHaveFocus();
    fireEvent.keyDown(trigger, { key: "ArrowUp" });
    fireEvent.keyDown(screen.getByRole("region"), { key: "Escape" });
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    fireEvent.keyDown(trigger, { key: "ArrowUp" });
    view.rerender(fixture("chat-2"));
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox")).toHaveValue("继续聊天");
    view.unmount();
    expect(document.querySelector(".ally-body-motion")).toBeNull();
  });
  it("大形象每轮覆盖 31 种状态、6 类身体动作和 15 类眼神，并避免重复", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "working", {
      reactions: companionReactions,
      preview: true,
    });
    playback.visibility(true, false);
    expect(
      companionReactions.filter((reaction) => reaction.group !== "action"),
    ).toHaveLength(31);
    expect(
      companionReactions.filter((reaction) => reaction.group === "action"),
    ).toHaveLength(21);
    let previous = "";
    for (let round = 0; round < 2; round++) {
      const seen = new Set<string>();
      for (let index = 0; index < companionReactions.length; index++) {
        engine.setEmotion.mockClear();
        engine.ball.perform.mockClear();
        playback.interact();
        const key = `${engine.setEmotion.mock.lastCall?.[0]}/${engine.ball.perform.mock.lastCall?.[0] ?? ""}`;
        expect(key).not.toBe(previous);
        seen.add(key);
        previous = key;
      }
      expect(seen.size).toBe(52);
    }
    vi.advanceTimersByTime(5000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("32");
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("长动作播完才恢复，预览不被开始收尾抢占，隐藏或新状态会打断", () => {
    vi.useFakeTimers();
    const engine = fakeEngine();
    const playback = companionPlayback(engine, "starting", {
      reactions: [
        {
          id: "test",
          emotion: "03",
          group: "action",
          action: "walk",
          duration: 3600,
        },
      ],
      preview: true,
    });
    playback.visibility(true, false);
    playback.interact();
    vi.advanceTimersByTime(2500);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("03");
    vi.advanceTimersByTime(1100);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("30");
    playback.interact();
    playback.visibility(false, false);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("30");
    playback.visibility(true, false);
    playback.interact();
    playback.update("failed");
    vi.advanceTimersByTime(5000);
    expect(engine.setEmotion).toHaveBeenLastCalledWith("34");
    playback.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("间隔太久的点击不会累积为唤出大形象", () => {
    vi.useFakeTimers();
    const view = render(
      <CompanionInteractionProvider
        scope="chat"
        activeAvatar={avatar}
        activity="idle"
      >
        <Avatar name="间隔伙伴" src={avatar} />
        <CompanionInteraction />
      </CompanionInteractionProvider>,
    );
    const trigger = screen.getByRole("button", { name: "与 间隔伙伴 互动" });
    fireEvent.click(trigger);
    vi.advanceTimersByTime(700);
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    view.unmount();
  });
});
