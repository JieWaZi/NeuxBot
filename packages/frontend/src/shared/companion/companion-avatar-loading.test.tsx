import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CompanionAvatar } from "./companion-avatar";
const mocks = vi.hoisted(() => ({ tinted: vi.fn() }));
vi.mock("@neuxbot/bloub/tint", () => mocks);
const decoding: {
  url: string;
  resolve: () => void;
  reject: (reason: Error) => void;
}[] = [];
let holdDecode = false;
beforeEach(() => {
  mocks.tinted.mockReset();
  decoding.length = 0;
  holdDecode = false;
  vi.stubGlobal(
    "Image",
    class {
      src = "";
      decode() {
        return new Promise<void>((resolve, reject) => {
          decoding.push({ url: this.src, resolve, reject });
          if (!holdDecode) resolve();
        });
      }
    },
  );
});
afterEach(() => vi.unstubAllGlobals());

it("同一头像切换材质保持视域和眼睛尺寸位置，不放大整张脸", async () => {
  mocks.tinted.mockResolvedValue("data:image/png;base64,aligned-material");
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:carre:complice:121314"
      animated={false}
    />,
  );
  await waitFor(() =>
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-texture-state",
      "ready",
    ),
  );
  const viewport = view.container.querySelector("svg")?.getAttribute("viewBox");
  const eyes = () =>
    Array.from(view.container.querySelectorAll("[data-eye]"), (eye) => [
      eye.getAttribute("d"),
      eye.getAttribute("transform"),
    ]);
  const before = eyes();
  view.rerender(
    <CompanionAvatar
      avatar="bloub:particles:carre:complice:121314"
      animated={false}
    />,
  );
  await waitFor(() =>
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-material",
      "particles",
    ),
  );
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "viewBox",
    viewport,
  );
  expect(eyes()).toEqual(before);
  const sprite = view.container.querySelector('[data-body-sprite="carre"]');
  expect(Number(sprite?.getAttribute("width"))).toBeGreaterThan(316);
  expect(Number(sprite?.getAttribute("x"))).toBeLessThan(-158);
});

it("首个纹理未准备好时隐藏整个头像，不显示平面身体或孤立眼睛", () => {
  mocks.tinted.mockReturnValue(new Promise(() => {}));
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:102030"
      animated={false}
    />,
  );
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "visibility",
    "hidden",
  );
});

it("切换外观时保留旧的完整身体和眼睛，纹理解码后一次提交新外观", async () => {
  let finish!: (value: string) => void;
  mocks.tinted
    .mockResolvedValueOnce("data:image/png;base64,previous")
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve;
        }),
    );
  const ready = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:203040"
      animated={false}
      onReady={ready}
    />,
  );
  await waitFor(() => expect(ready).toHaveBeenCalledOnce());
  const eyes = Array.from(
    view.container.querySelectorAll("[data-eye]"),
    (eye) => eye.getAttribute("d"),
  );
  holdDecode = true;
  view.rerender(
    <CompanionAvatar
      avatar="bloub:particles:hexagone:curieux:203040"
      animated={false}
      onReady={ready}
    />,
  );
  const oldAppearance = () => {
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-material",
      "plush",
    );
    expect(view.container.querySelector("svg")).toHaveAttribute(
      "data-shape",
      "cercle",
    );
    expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      "data:image/png;base64,previous",
    );
    expect(
      Array.from(view.container.querySelectorAll("[data-eye]"), (eye) =>
        eye.getAttribute("d"),
      ),
    ).toEqual(eyes);
  };
  oldAppearance();
  await act(async () => finish("data:image/png;base64,next"));
  oldAppearance();
  expect(ready).toHaveBeenCalledOnce();
  const pending = decoding.find(
    ({ url }) => url === "data:image/png;base64,next",
  );
  if (!pending) throw new Error("缺少新纹理解码");
  await act(async () => pending.resolve());
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-material",
    "particles",
  );
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-shape",
    "hexagone",
  );
  expect(view.container.querySelector("svg")).not.toHaveAttribute(
    "visibility",
    "hidden",
  );
  expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    "data:image/png;base64,next",
  );
  expect(ready).toHaveBeenCalledTimes(2);
});

it("已准备的纹理重挂载同步可见，相同材质颜色的形状和表情切换不重新准备", async () => {
  mocks.tinted.mockResolvedValue("data:image/png;base64,warm");
  const ready = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:405060"
      animated={false}
      onReady={ready}
    />,
  );
  await waitFor(() => expect(ready).toHaveBeenCalledOnce());
  view.unmount();
  const remounted = render(
    <CompanionAvatar
      avatar="bloub:plush:hexagone:curieux:405060"
      animated={false}
    />,
  );
  expect(remounted.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    "data:image/png;base64,warm",
  );
  expect(remounted.container.querySelector("svg")).not.toHaveAttribute(
    "visibility",
    "hidden",
  );
  expect(remounted.container.querySelector("svg")).toHaveAttribute(
    "data-shape",
    "hexagone",
  );
  expect(mocks.tinted).toHaveBeenCalledOnce();
  expect(decoding).toHaveLength(1);
});

it("同一纹理的多个缩略图共享换色与解码，只在解码完成后一起显示", async () => {
  let finish!: (value: string) => void;
  mocks.tinted.mockImplementation(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  holdDecode = true;
  const view = render(
    <>
      <CompanionAvatar
        avatar="bloub:plush:cercle:neutre:506070"
        animated={false}
      />
      <CompanionAvatar
        avatar="bloub:plush:hexagone:curieux:506070"
        animated={false}
      />
      <CompanionAvatar
        avatar="bloub:plush:triangle:reveur:506070"
        animated={false}
      />
    </>,
  );
  expect(mocks.tinted).toHaveBeenCalledOnce();
  await act(async () => finish("data:image/png;base64,shared"));
  expect(decoding).toHaveLength(1);
  expect(
    Array.from(view.container.querySelectorAll(":scope > span > svg"), (svg) =>
      svg.getAttribute("visibility"),
    ),
  ).toEqual(["hidden", "hidden", "hidden"]);
  await act(async () => decoding[0]?.resolve());
  expect(
    view.container.querySelectorAll(
      "[data-texture][href='data:image/png;base64,shared']",
    ),
  ).toHaveLength(3);
});

it("超过12个就绪纹理后淘汰最久未用项，保留新近项、当前显示和在途去重", async () => {
  let finishPending!: (value: string) => void;
  const pending = new Promise<string>((resolve) => {
    finishPending = resolve;
  });
  const url = (color: string) => `data:image/png;base64,${color}`;
  mocks.tinted.mockImplementation((_source: string, color: string) =>
    color === "#c01000" ? pending : Promise.resolve(url(color)),
  );
  const waiting = render(
    <>
      <CompanionAvatar
        avatar="bloub:plush:cercle:neutre:c01000"
        animated={false}
      />
      <CompanionAvatar
        avatar="bloub:plush:triangle:curieux:c01000"
        animated={false}
      />
    </>,
  );
  const avatar = (index: number) =>
    `bloub:plush:cercle:neutre:${(0xc00000 + index).toString(16)}`;
  const view = render(<CompanionAvatar avatar={avatar(1)} animated={false} />);
  await waitFor(() =>
    expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      url("#c00001"),
    ),
  );
  const retained = render(
    <CompanionAvatar avatar={avatar(2)} animated={false} />,
  );
  await waitFor(() =>
    expect(retained.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      url("#c00002"),
    ),
  );
  for (let index = 2; index <= 12; index++)
    await act(async () =>
      view.rerender(
        <CompanionAvatar avatar={avatar(index)} animated={false} />,
      ),
    );

  // 第一项重新使用，第二项成为最久未用的就绪纹理。
  const touched = render(
    <CompanionAvatar avatar={avatar(1)} animated={false} />,
  );
  expect(touched.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#c00001"),
  );
  touched.unmount();
  await act(async () =>
    view.rerender(<CompanionAvatar avatar={avatar(13)} animated={false} />),
  );
  const recent = render(
    <CompanionAvatar avatar={avatar(1)} animated={false} />,
  );
  expect(recent.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#c00001"),
  );
  const cold = render(<CompanionAvatar avatar={avatar(2)} animated={false} />);
  expect(cold.container.querySelector("svg")).toHaveAttribute(
    "visibility",
    "hidden",
  );
  expect(retained.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#c00002"),
  );
  expect(retained.container.querySelector("svg")).not.toHaveAttribute(
    "visibility",
    "hidden",
  );
  await waitFor(() =>
    expect(cold.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      url("#c00002"),
    ),
  );
  expect(decoding.filter((image) => image.url === url("#c00002"))).toHaveLength(
    2,
  );
  expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#c0000d"),
  );
  expect(
    mocks.tinted.mock.calls.filter(([, color]) => color === "#c01000"),
  ).toHaveLength(1);
  await act(async () => finishPending(url("#c01000")));
  expect(decoding.filter((image) => image.url === url("#c01000"))).toHaveLength(
    1,
  );
  expect(
    waiting.container.querySelectorAll(
      "[data-texture][href='data:image/png;base64,#c01000']",
    ),
  ).toHaveLength(2);
});

it("新纹理失败后提交新请求的明确降级，不永久保留旧外观", async () => {
  mocks.tinted
    .mockResolvedValueOnce("data:image/png;base64,good")
    .mockRejectedValueOnce(new Error("tint failed"));
  const ready = vi.fn();
  const error = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:708090"
      animated={false}
      onReady={ready}
      onError={error}
    />,
  );
  await waitFor(() => expect(ready).toHaveBeenCalledOnce());
  view.rerender(
    <CompanionAvatar
      avatar="bloub:particles:hexagone:curieux:8090a0"
      animated={false}
      onReady={ready}
      onError={error}
    />,
  );
  await waitFor(() => expect(error).toHaveBeenCalledOnce());
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-material",
    "particles",
  );
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-shape",
    "hexagone",
  );
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-texture-state",
    "error",
  );
  expect(view.container.querySelector("svg")).not.toHaveAttribute(
    "visibility",
    "hidden",
  );
  expect(view.container.querySelector("[data-texture]")).toBeNull();
  expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
});

it("同一请求跨实例恢复成功后，缓存淘汰和再次渲染不会退回失败平面", async () => {
  const url = (color: string) => `data:image/png;base64,${color}`;
  mocks.tinted
    .mockRejectedValueOnce(new Error("transient"))
    .mockImplementation((_source: string, color: string) =>
      Promise.resolve(url(color)),
    );
  const avatar = (index: number) =>
    `bloub:plush:cercle:neutre:${(0xf10000 + index).toString(16)}`;
  const original = render(
    <CompanionAvatar avatar={avatar(0)} animated={false} />,
  );
  await waitFor(() =>
    expect(original.container.querySelector("svg")).toHaveAttribute(
      "data-texture-state",
      "error",
    ),
  );
  const retry = render(<CompanionAvatar avatar={avatar(0)} animated={false} />);
  await waitFor(() =>
    expect(retry.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      url("#f10000"),
    ),
  );
  retry.unmount();
  original.rerender(<CompanionAvatar avatar={avatar(0)} animated={false} />);
  expect(original.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#f10000"),
  );
  const filler = render(
    <CompanionAvatar avatar={avatar(1)} animated={false} />,
  );
  await waitFor(() =>
    expect(filler.container.querySelector("[data-texture]")).toHaveAttribute(
      "href",
      url("#f10001"),
    ),
  );
  for (let index = 2; index <= 13; index++)
    await act(async () =>
      filler.rerender(
        <CompanionAvatar avatar={avatar(index)} animated={false} />,
      ),
    );
  original.rerender(<CompanionAvatar avatar={avatar(0)} animated={false} />);
  expect(original.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    url("#f10000"),
  );
  expect(original.container.querySelector("[data-body]")).toHaveAttribute(
    "opacity",
    "0",
  );
  expect(original.container.querySelector("svg")).toHaveAttribute(
    "data-texture-state",
    "ready",
  );
});

it("浏览器拒绝解码时不提前报告就绪，并交接明确失败降级", async () => {
  mocks.tinted.mockResolvedValue("data:image/png;base64,broken");
  holdDecode = true;
  const ready = vi.fn();
  const error = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:90a0b0"
      animated={false}
      onReady={ready}
      onError={error}
    />,
  );
  await waitFor(() => expect(decoding).toHaveLength(1));
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "visibility",
    "hidden",
  );
  expect(ready).not.toHaveBeenCalled();
  await act(async () => decoding[0]?.reject(new Error("decode failed")));
  expect(error).toHaveBeenCalledOnce();
  expect(ready).not.toHaveBeenCalled();
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-texture-state",
    "error",
  );
});

it("快速变更颜色后迟到的纹理不会覆盖最新选择", async () => {
  let first!: (value: string) => void;
  let second!: (value: string) => void;
  mocks.tinted
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          first = resolve;
        }),
    )
    .mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          second = resolve;
        }),
    );
  const ready = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:8b5cf6"
      animated={false}
      onReady={ready}
    />,
  );
  view.rerender(
    <CompanionAvatar
      avatar="bloub:particles:hexagone:curieux:38bdf8"
      animated={false}
      onReady={ready}
    />,
  );
  await act(async () => second("data:image/png;base64,new"));
  expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    "data:image/png;base64,new",
  );
  await act(async () => first("data:image/png;base64,old"));
  expect(view.container.querySelector("[data-texture]")).toHaveAttribute(
    "href",
    "data:image/png;base64,new",
  );
  expect(ready).toHaveBeenCalledOnce();
});
it("纹理失败保留完整形状与眼睛，并提供失败交接", async () => {
  mocks.tinted.mockRejectedValue(new Error("image unavailable"));
  const error = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:particles:cercle:neutre:8b5cf6"
      animated={false}
      onError={error}
    />,
  );
  await waitFor(() => expect(error).toHaveBeenCalledOnce());
  expect(view.container.querySelector("[data-body]")).toBeInTheDocument();
  expect(view.container.querySelectorAll("[data-eye]")).toHaveLength(2);
});
it("卸载后纹理完成不再通知调用方", async () => {
  let finish!: (value: string) => void;
  mocks.tinted.mockImplementation(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  const ready = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar="bloub:plush:cercle:neutre:607080"
      onReady={ready}
    />,
  );
  view.unmount();
  await act(async () => finish("data:image/png;base64,late"));
  expect(ready).not.toHaveBeenCalled();
});
