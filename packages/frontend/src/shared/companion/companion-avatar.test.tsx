import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CompanionAvatar } from "./companion-avatar";
import { defaultCompanionAvatar } from "./appearance";
import { SEQUENCE } from "@neuxbot/bloub/states";
import { BotEngine } from "@neuxbot/bloub/engine";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { enlargedEyes } from "@neuxbot/bloub/eyes";
vi.mock("./prepared-appearance", async () => {
  const { companionAppearance } = await import("./appearance");
  return {
    usePreparedAppearance: (request: string) => ({
      request,
      appearance: companionAppearance(request),
      status: "ready",
      texture: "data:image/png;base64,test",
    }),
  };
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("通用头像渲染", () => {
  it("外观预览持续播放轻微动态，但不轮换眼睛或响应悬停点击换表情", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.6);
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    const raf = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation((callback) => {
        next = callback;
        return 1;
      });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar="bloub:plush:nuage:curieux:38bdf8" preview />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.pointerEnter(host);
    fireEvent.click(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    const movement = view.container
      .querySelector("[data-body]")
      ?.getAttribute("d");
    for (let index = 0; index < 210; index++) {
      now += 100;
      act(() => {
        vi.advanceTimersByTime(100);
        next?.(now);
      });
      expect(view.container.querySelector("svg")).toHaveAttribute(
        "data-expression",
        "curieux",
      );
    }
    expect(raf.mock.calls.length).toBeGreaterThan(200);
    expect(
      view.container.querySelector("[data-body]")?.getAttribute("d"),
    ).not.toBe(movement);
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("空闲保持保存的眼睛，悬停随机表情持续保持，移开立即恢复", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.6);
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar="bloub:plush:nuage:curieux:38bdf8" />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    const svg = view.container.querySelector("svg");
    expect(svg).toHaveAttribute("data-expression", "curieux");
    act(() => vi.advanceTimersByTime(120_000));
    expect(svg).toHaveAttribute("data-expression", "curieux");

    fireEvent.pointerEnter(host);
    const hoverExpression = svg?.getAttribute("data-expression");
    expect(hoverExpression).not.toBe("curieux");
    act(() => vi.advanceTimersByTime(120_000));
    expect(svg).toHaveAttribute("data-expression", hoverExpression);
    fireEvent.pointerLeave(host);
    expect(svg).toHaveAttribute("data-expression", "curieux");
    expect(svg).toHaveAttribute("data-animation", "idle");
    expect(vi.getTimerCount()).toBe(0);

    fireEvent.pointerEnter(host);
    expect(svg?.getAttribute("data-expression")).not.toBe(hoverExpression);
    fireEvent.pointerLeave(host);
    expect(svg).toHaveAttribute("data-expression", "curieux");
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("悬停时修改保存的表情，移开后使用最新选择", () => {
    vi.spyOn(Math, "random").mockReturnValue(0.6);
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar="bloub:plush:nuage:curieux:38bdf8" />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.pointerEnter(host);
    view.rerender(<CompanionAvatar avatar="bloub:plush:nuage:timide:38bdf8" />);
    fireEvent.pointerLeave(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-expression",
      "timide",
    );
  });
  it.each([
    ["静态缩略图", false, false, "idle", 0],
    ["减少动态效果", true, true, "idle", 0],
    ["工作状态", true, false, "working", 0],
  ] as const)(
    "%s的悬停入口不创建额外定时器",
    (_label, animated, reduced, activity, timers) => {
      vi.useFakeTimers();
      vi.stubGlobal("matchMedia", () => ({
        matches: reduced,
        addEventListener: () => {},
        removeEventListener: () => {},
      }));
      vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
      vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
      const view = render(
        <CompanionAvatar
          avatar={defaultCompanionAvatar}
          animated={animated}
          activity={activity}
        />,
      );
      const host = view.container.firstElementChild;
      if (!host) throw new Error("缺少头像容器");
      expect(vi.getTimerCount()).toBe(timers);
      fireEvent.pointerEnter(host);
      expect(vi.getTimerCount()).toBe(timers);
      expect(view.container.querySelector("svg")).not.toHaveAttribute(
        "data-animation",
        "wink",
      );
    },
  );
  it("更新工作头像外观仍保持工具执行动作", () => {
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="working" />,
    );
    view.rerender(
      <CompanionAvatar
        avatar="bloub:plush:hexagone:reveur:8b5cf6"
        activity="working"
      />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "thinking",
    );
  });
  it("准备与工具阶段统一保持原生思考，不切换彗星或其他形状", () => {
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="connecting" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "thinking",
    );
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="working" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "thinking",
    );
  });
  it("完成动作优先于点击反应", () => {
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="working" />,
    );
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="completed" />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.click(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "notify",
    );
  });
  it.each(["减少动态", "暂停预览", "隐藏"])(
    "眨眼闭合时%s只按暂停原因处理固定姿态，恢复不回退时钟",
    (reason) => {
      let next: FrameRequestCallback | undefined;
      let now = 0;
      const media = new EventTarget();
      let reduced = false;
      vi.stubGlobal("matchMedia", () => ({
        get matches() {
          return reduced;
        },
        addEventListener: media.addEventListener.bind(media),
        removeEventListener: media.removeEventListener.bind(media),
      }));
      const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(false);
      vi.spyOn(performance, "now").mockImplementation(() => now);
      const raf = vi
        .spyOn(window, "requestAnimationFrame")
        .mockImplementation((callback) => {
          next = callback;
          return 1;
        });
      const cancel = vi
        .spyOn(window, "cancelAnimationFrame")
        .mockImplementation(() => {});
      const view = render(
        <CompanionAvatar avatar={defaultCompanionAvatar} animation="idle" />,
      );
      const eyes = () =>
        Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
          eye.getAttribute("transform"),
        );
      const opening = () =>
        eyes().map((matrix) => {
          const values = matrix?.slice(7, -1).split(",").map(Number);
          // 闭合程度按原尺寸判断，不把可读性放大计入眨眼开合。
          return Math.hypot(values?.[1] ?? 0, values?.[3] ?? 0) / 1.3;
        });
      for (let index = 0; index < 14; index++) {
        now += 100;
        act(() => next?.(now));
      }
      now = 1481;
      act(() => next?.(now));
      expect(opening().every((value) => value < 0.07)).toBe(true);
      const closed = eyes();
      const requests = raf.mock.calls.length;
      if (reason === "减少动态")
        act(() => {
          reduced = true;
          media.dispatchEvent(new Event("change"));
        });
      else if (reason === "暂停预览")
        view.rerender(
          <CompanionAvatar
            avatar={defaultCompanionAvatar}
            animation="idle"
            animated={false}
          />,
        );
      else
        act(() => {
          hidden.mockReturnValue(true);
          document.dispatchEvent(new Event("visibilitychange"));
        });
      expect(cancel).toHaveBeenCalledWith(1);
      expect(raf).toHaveBeenCalledTimes(requests);
      if (reason === "隐藏") expect(eyes()).toEqual(closed);
      else expect(opening().every((value) => value > 0.4)).toBe(true);
      const frozen = eyes();
      if (reason === "减少动态")
        act(() => {
          reduced = false;
          media.dispatchEvent(new Event("change"));
        });
      else if (reason === "暂停预览")
        view.rerender(
          <CompanionAvatar avatar={defaultCompanionAvatar} animation="idle" />,
        );
      else
        act(() => {
          hidden.mockReturnValue(false);
          document.dispatchEvent(new Event("visibilitychange"));
        });
      expect(eyes()).toEqual(frozen);
    },
  );
  it("原生动画可独立预览，固定模式不用动画循环也有可辨认的姿态", () => {
    const raf = vi.spyOn(window, "requestAnimationFrame");
    const view = render(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        animated={false}
        animation="idle"
      />,
    );
    for (const animation of SEQUENCE) {
      view.rerender(
        <CompanionAvatar
          avatar={defaultCompanionAvatar}
          animated={false}
          animation={animation}
        />,
      );
      expect(view.container.querySelector("svg")).toHaveAttribute(
        "data-animation",
        animation === "sleep" ? "idle" : animation,
      );
      expect(
        view.container.querySelector("[data-body]")?.getAttribute("d"),
      ).toMatch(/^M/);
      if (["thinking", "alert", "exclaim", "burst"].includes(animation))
        expect(
          view.container.querySelector("[data-native-dot]"),
        ).toBeInTheDocument();
    }
    expect(raf).not.toHaveBeenCalled();
  });
  it("空闲超过90秒仍保留完整身体，旧resting与sleep输入兼容为空闲", async () => {
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
    await act(async () => vi.advanceTimersByTime(91_000));
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "idle",
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    expect(
      view.container.querySelector("[data-body-sprite]"),
    ).toBeInTheDocument();
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
    view.rerender(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        activity="resting"
        animated={false}
      />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "idle",
    );
    view.rerender(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        animated={false}
        animation="sleep"
      />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
  });
  it("工具执行保持原生动作，失败立即退出执行姿态", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.6);
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      next = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="working" />,
    );
    for (let index = 0; index < 81; index++) {
      now += 100;
      act(() => {
        vi.advanceTimersByTime(100);
        next?.(now);
      });
    }
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "thinking",
    );
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="failed" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "alert",
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "failed",
    );
  });
  it("原生预览覆盖业务辅助视线，保留蛋形原生眼睛姿态", () => {
    const view = render(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        animated={false}
        activity="working"
        animation="egg"
      />,
    );
    const expected = enlargedEyes(
      enlargedEyes(new BotEngine(100, "egg").sample(0.8).eyes, 1.3),
    ).map((eye) => eye.matrix);
    expect(
      Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
        eye.getAttribute("transform"),
      ),
    ).toEqual(expected);
  });
  it("工具执行及等待不接受点击覆盖", () => {
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      next = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="working" />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.click(host);
    for (let index = 0; index < 8; index++) {
      now += 100;
      act(() => next?.(now));
    }
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "thinking",
    );
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(0);
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="waiting" />,
    );
    fireEvent.click(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
  });
  it("原生警示预览跨越完整动作后重新运动，不停留在末尾姿态", () => {
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      next = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} animation="alert" />,
    );
    for (let index = 0; index < 32; index++) {
      now += 100;
      act(() => next?.(now));
    }
    const dot = view.container
      .querySelector("[data-native-dot]")
      ?.getAttribute("transform");
    expect(Number(dot?.match(/^translate\(([^,]+)/u)?.[1])).toBeGreaterThan(15);
  });
  it.each(["idle", "working"] as const)(
    "隐藏时释放%s绘图与表情定时器，卸载后不遗留定时器",
    (activity) => {
      vi.useFakeTimers();
      const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(false);
      vi.spyOn(window, "requestAnimationFrame").mockReturnValue(7);
      const cancel = vi
        .spyOn(window, "cancelAnimationFrame")
        .mockImplementation(() => {});
      const view = render(
        <CompanionAvatar avatar={defaultCompanionAvatar} activity={activity} />,
      );
      expect(vi.getTimerCount()).toBe(0);
      act(() => {
        hidden.mockReturnValue(true);
        document.dispatchEvent(new Event("visibilitychange"));
      });
      expect(cancel).toHaveBeenCalledWith(7);
      expect(vi.getTimerCount()).toBe(0);
      view.unmount();
      expect(vi.getTimerCount()).toBe(0);
    },
  );
  it("正文眨眼切换为完成通知，不叠加自定义跳动", () => {
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      next = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="responding" />,
    );
    const tick = () => {
      now += 100;
      act(() => {
        if (!next) throw new Error("缺少动画帧");
        next(now);
      });
    };
    for (let index = 0; index < 15; index++) tick();
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="completed" />,
    );
    tick();
    const transform = view.container
      .querySelector("[data-body-material]")
      ?.getAttribute("transform");
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "notify",
    );
    expect(transform).toBeNull();
  });
  it("固定帧切回播放不回退已呈现的失败表情", () => {
    const view = render(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        activity="failed"
        animated={false}
      />,
    );
    const eyes = () =>
      Array.from(
        view.container.querySelectorAll("[data-eye]"),
        (eye) => `${eye.getAttribute("d")}:${eye.getAttribute("transform")}`,
      );
    const frozen = eyes();
    view.rerender(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        activity="failed"
        animated
      />,
    );
    expect(eyes()).toEqual(frozen);
  });
  it("固定帧不创建动画循环，所有材质都有完整轮廓和两只眼睛", async () => {
    const raf = vi.spyOn(window, "requestAnimationFrame");
    const ready = vi.fn();
    const view = render(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        animated={false}
        onReady={ready}
      />,
    );
    await waitFor(() => expect(ready).toHaveBeenCalled());
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-material",
      "plush",
    );
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
    expect(view.container.querySelector("[data-body]")).toHaveAttribute("d");
    expect(raf).not.toHaveBeenCalled();
  });
  it("等待和失败立即打断悬停，持续显示真实状态", async () => {
    const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.pointerEnter(host);
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="failed" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "failed",
    );
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="waiting" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "waiting",
    );
  });
  it("已完成消息挂载即显示通知，并持续保持，不被定时器或交互覆盖", () => {
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="completed" />,
    );
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.click(host);
    fireEvent.pointerEnter(host);
    act(() => vi.advanceTimersByTime(120_000));
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "notify",
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "completed",
    );
    expect(
      view.container.querySelector("[data-native-notification]"),
    ).toBeInTheDocument();
    expect(vi.getTimerCount()).toBe(0);
  });
});

it("真实阶段控制原生动作，持续工作及交互不触发动作和表情轮播", () => {
  vi.useFakeTimers();
  let next: FrameRequestCallback | undefined;
  let now = 0;
  vi.spyOn(performance, "now").mockImplementation(() => now);
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    next = callback;
    return 1;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
  const view = render(
    <CompanionAvatar avatar={defaultCompanionAvatar} activity="thinking" />,
  );
  for (const [activity, animation] of [
    ["thinking", "thinking"],
    ["working", "thinking"],
    ["searching", "thinking"],
    ["responding", "wink"],
  ] as const) {
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity={activity} />,
    );
    const expression = view.container
      .querySelector("svg")
      ?.getAttribute("data-expression");
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.click(host);
    fireEvent.pointerEnter(host);
    for (let index = 0; index < 200; index++) {
      now += 100;
      act(() => {
        vi.advanceTimersByTime(100);
        next?.(now);
      });
      expect(view.container.querySelector("svg")).toHaveAttribute(
        "data-animation",
        animation,
      );
      expect(view.container.querySelector("svg")).toHaveAttribute(
        "data-expression",
        expression,
      );
      expect(
        view.container
          .querySelector("[data-body-material]")
          ?.getAttribute("transform"),
      ).toBeNull();
    }
    expect(vi.getTimerCount()).toBe(0);
  }
});

it.each(["responding"] as const)(
  "%s 的原生身体保持保存的六边形轮廓",
  (activity) => {
    let next: FrameRequestCallback | undefined;
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      next = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const avatar = "bloub:plush:hexagone:neutre:8b5cf6";
    const view = render(
      <CompanionAvatar avatar={avatar} activity="thinking" />,
    );
    view.rerender(<CompanionAvatar avatar={avatar} activity={activity} />);
    for (let index = 0; index < 20; index++) {
      now += 100;
      act(() => next?.(now));
    }
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-shape",
      "hexagone",
    );
    expect(
      view.container.querySelector('[data-body-sprite="hexagone"]'),
    ).toBeInTheDocument();
    // 保留素材标签还不够：原生身体不得把六边形径向变形成圆形或其他轮廓。
    expect(
      view.container.querySelector('[data-body-warp="radial-svg"]'),
    ).toBeNull();
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      activity === "responding" ? "wink" : "notify",
    );
  },
);

it("思考与工具执行使用同一思考动作，阶段交接不重启动作", () => {
  let now = 0;
  let sequence = 0;
  const callbacks = new Map<number, FrameRequestCallback>();
  vi.spyOn(performance, "now").mockImplementation(() => now);
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    callbacks.set(++sequence, callback);
    return sequence;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
    callbacks.delete(id);
  });
  const avatars = (activity: "thinking" | "working") => (
    <>
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="thinking" />
      <CompanionAvatar avatar={defaultCompanionAvatar} activity={activity} />
    </>
  );
  const view = render(avatars("thinking"));
  const advance = () => {
    now += 100;
    const frameCallbacks = [...callbacks.values()];
    callbacks.clear();
    act(() => frameCallbacks.forEach((callback) => callback(now)));
  };
  for (let index = 0; index < 9; index++) advance();
  view.rerender(avatars("working"));
  for (let index = 0; index < 9; index++) {
    advance();
    const geometry = [...view.container.children].map((host) => {
      const svg = host.querySelector("svg[data-companion-avatar]");
      if (!svg) throw new Error("缺少头像");
      return [...svg.querySelectorAll("[data-body], [data-native-dot]")].map(
        (node) =>
          ["d", "cx", "cy", "r"].map((attribute) =>
            node.getAttribute(attribute),
          ),
      );
    });
    expect(geometry[1]).toEqual(geometry[0]);
  }
});

it("通知使用原生galet斜卵石轮廓与对应素材，不强制改成正圆", () => {
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:hexagone:neutre:8b5cf6"
      activity="completed"
      animated={false}
    />,
  );
  const native = new BotEngine(
    100,
    "notify",
    SHAPE_BY_ID.get("galet")?.radii,
  ).sample(1.4);
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-animation",
    "notify",
  );
  expect(view.container.querySelector("[data-body]")).toHaveAttribute(
    "d",
    native.bodyPath,
  );
  expect(
    view.container.querySelector("[data-native-notification]"),
  ).toBeInTheDocument();
  expect(
    view.container.querySelector('[data-body-sprite="galet"]'),
  ).toBeInTheDocument();
  expect(
    view.container.querySelector('[data-body-sprite="hexagone"]'),
  ).toBeNull();
  expect(
    view.container.querySelector('[data-body-warp="radial-svg"]'),
  ).toBeNull();
});
