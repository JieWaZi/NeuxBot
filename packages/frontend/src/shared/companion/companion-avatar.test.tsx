import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CompanionAvatar } from "./companion-avatar";
import { defaultCompanionAvatar } from "./appearance";
import { SEQUENCE } from "@neuxbot/bloub/states";
import { BotEngine } from "@neuxbot/bloub/engine";
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
  it("持续悬停只短暂眨眼，之后实际眼睛轮廓继续自动轮换", () => {
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
    const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.pointerEnter(host);
    const paths = () =>
      Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
        eye.getAttribute("d"),
      );
    const samples: ReturnType<typeof paths>[] = [];
    for (const target of [1000, 5000, 9000, 13000, 16000]) {
      while (now < target) {
        now += 100;
        act(() => {
          vi.advanceTimersByTime(100);
          next?.(now);
        });
      }
      samples.push(paths());
    }
    expect(samples.every((eyes) => eyes.length === 2)).toBe(true);
    for (let index = 1; index < samples.length; index++)
      expect(samples[index]).not.toEqual(samples[index - 1]);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
  });
  it("离开头像立即取消悬停反馈，卸载释放悬停定时器", () => {
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockReturnValue(1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.pointerEnter(host);
    expect(vi.getTimerCount()).toBe(2);
    fireEvent.pointerLeave(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    expect(vi.getTimerCount()).toBe(1);
    fireEvent.pointerEnter(host);
    expect(vi.getTimerCount()).toBe(2);
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it.each([
    ["静态缩略图", false, false, "idle", 0],
    ["减少动态效果", true, true, "idle", 0],
    ["工作状态", true, false, "working", 1],
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
  it("空闲持续改变实际眼睛轮廓，静态缩略图保留保存的表情", () => {
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
    const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
    const paths = () =>
      Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
        eye.getAttribute("d"),
      );
    const first = paths();
    for (let index = 0; index < 55; index++) {
      now += 100;
      act(() => {
        vi.advanceTimersByTime(100);
        next?.(now);
      });
    }
    expect(paths()).toHaveLength(2);
    expect(paths()).not.toEqual(first);
    view.unmount();
    const thumbnail = render(
      <CompanionAvatar avatar={defaultCompanionAvatar} animated={false} />,
    );
    expect(thumbnail.container.querySelector("svg")).toHaveAttribute(
      "data-expression",
      "neutre",
    );
    expect(vi.getTimerCount()).toBe(0);
  });
  it("工作中的轮换表情实际改变眼睛几何，原生动作之间持续有表情段", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.24);
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
    const advance = (steps: number) => {
      for (let index = 0; index < steps; index++) {
        now += 100;
        act(() => {
          vi.advanceTimersByTime(100);
          next?.(now);
        });
      }
    };
    advance(7);
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
    expect(
      view.container.querySelector("[data-eye]")?.getAttribute("d"),
    ).toMatch(/^M-10\.5 -11\.5A10\.5 10\.5/);
    advance(12);
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
    expect(
      view.container.querySelector("[data-eye]")?.getAttribute("d"),
    ).toMatch(/^M-8 -9A8 8/);
    advance(62);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "orbit",
    );
    view.rerender(
      <CompanionAvatar
        avatar="bloub:v1:plush:hexagone:reveur:8b5cf6"
        activity="working"
      />,
    );
    advance(5);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "orbit",
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
      "exclaim",
    );
  });
  it("点击表情在1.8秒后恢复当前工作动作及其时间位置", () => {
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
    const advance = (steps: number) => {
      for (let index = 0; index < steps; index++) {
        now += 100;
        act(() => {
          vi.advanceTimersByTime(100);
          next?.(now);
        });
      }
    };
    advance(81);
    const host = view.container.firstElementChild;
    if (!host) throw new Error("缺少头像容器");
    fireEvent.click(host);
    advance(7);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    expect(
      view.container.querySelector("[data-eye]")?.getAttribute("d"),
    ).toMatch(/^M-15 0A6 6/);
    advance(10);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
    );
    advance(2);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "orbit",
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
          return Math.hypot(values?.[1] ?? 0, values?.[3] ?? 0);
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
  it("工作表情轮换不重启原生动作时间线，失败会立即取消当前小剧场", () => {
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
      "orbit",
    );
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="failed" />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "idle",
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
      new BotEngine(100, "egg").sample(0.8).eyes,
    ).map((eye) => eye.matrix);
    expect(
      Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
        eye.getAttribute("transform"),
      ),
    ).toEqual(expected);
  });
  it("工作中点击临时显示新表情，等待仍优先且不接受互动覆盖", () => {
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
      "idle",
    );
    expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
    view.rerender(
      <CompanionAvatar avatar={defaultCompanionAvatar} activity="waiting" />,
    );
    fireEvent.click(host);
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-animation",
      "wide",
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
      expect(vi.getTimerCount()).toBe(1);
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
  it("回答超过一秒后完成仍从完成动作起点播放强调", () => {
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
    expect(transform).toMatch(/^translate/);
    expect(
      Number(transform?.match(/^translate\([^,]+,([^)]*)/u)?.[1]),
    ).toBeLessThan(0);
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
  it("历史完成消息不重复庆祝，真实完成转换短暂强调后回空闲", () => {
    vi.useFakeTimers();
    const view = render(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        activity="completed"
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
        activity="working"
        animated={false}
      />,
    );
    view.rerender(
      <CompanionAvatar
        avatar={defaultCompanionAvatar}
        activity="completed"
        animated={false}
      />,
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-activity",
      "completed",
    );
    vi.useRealTimers();
  });
});
