import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useRef,
  useState,
  type ReactNode,
  type Ref,
  type SVGProps,
} from "react";

import "./ally-avatar-studio.css";

/**
 * NeuxBot Avatar Studio · v10
 * 方向：单轮廓、眼睛和动作分开编排，使用 NeuxBot 自己的轮廓、配色和交互。
 */

const clamp = (v: number, min: number, max: number) =>
  Math.max(min, Math.min(max, v));
const PI = Math.PI;
const valid = <T extends string>(
  value: string,
  catalog: Record<T, unknown>,
  fallback: T,
) =>
  Object.prototype.hasOwnProperty.call(catalog, value)
    ? (value as T)
    : fallback;

function mix(a: string, b: string, t: number) {
  const va = parseInt(a.slice(1), 16);
  const vb = parseInt(b.slice(1), 16);
  return (
    "#" +
    [16, 8, 0]
      .map((shift) => {
        const av = (va >> shift) & 255;
        const bv = (vb >> shift) & 255;
        return Math.round(av * (1 - t) + bv * t)
          .toString(16)
          .padStart(2, "0");
      })
      .join("")
  );
}

function makeTheme(base: string) {
  return {
    base,
    accent: mix(base, "#000000", 0.16),
    soft: mix(base, "#ffffff", 0.22),
    glow: mix(base, "#ffffff", 0.6),
    shadow: mix(base, "#0f1220", 0.55),
    eye: "#151822",
  };
}

export const SHAPES = {
  pebble: {
    label: "圆润石",
    desc: "稳定、友好、百搭",
    d: "M76 230C54 208 49 171 59 141C72 102 111 80 151 80C191 80 225 98 248 128C266 152 275 185 268 211C258 244 219 264 173 265C134 266 97 254 76 230Z",
    eyes: { left: [136, 168], right: [184, 168], scale: 1 },
    top: [160, 84],
  },
  wedge: {
    label: "弧角锥",
    desc: "敏捷、专注、干练",
    d: "M78 236C63 218 61 193 72 175L143 87C152 76 168 72 180 79L250 121C261 128 269 139 273 152L292 223C296 237 288 250 274 254L132 268C106 270 89 259 78 236Z",
    eyes: { left: [145, 171], right: [191, 171], scale: 0.96 },
    top: [173, 84],
  },
  droplet: {
    label: "灵滴",
    desc: "轻盈、好奇、灵动",
    d: "M158 70C176 83 215 118 238 154C260 189 263 229 235 252C206 276 113 275 84 252C58 231 61 188 81 157C103 122 139 84 158 70Z",
    eyes: { left: [139, 178], right: [181, 178], scale: 0.98 },
    top: [159, 74],
  },
  kite: {
    label: "风筝体",
    desc: "理性、清晰、有方向感",
    d: "M158 70C171 73 182 81 191 95L254 189C261 199 262 212 257 223L225 249C217 256 205 260 194 261L126 258C115 258 104 253 96 245L66 201C60 190 60 177 67 166L122 96C130 83 144 72 158 70Z",
    eyes: { left: [139, 170], right: [181, 170], scale: 0.94 },
    top: [160, 74],
  },
  puff: {
    label: "绒团体",
    desc: "柔和、陪伴、治愈",
    d: "M74 251C47 251 32 235 32 213C32 193 45 178 64 175C69 143 95 123 124 124C138 124 151 129 162 138C175 119 195 110 218 110C256 110 285 136 286 172C305 178 316 193 316 213C316 238 296 253 265 253H74Z",
    eyes: { left: [141, 184], right: [183, 184], scale: 0.96 },
    top: [162, 112],
  },
};

export const COLORS = {
  pearl: { label: "珍珠白", base: "#F1EEE6" },
  blue: { label: "天空蓝", base: "#92B7FF" },
  peach: { label: "蜜桃粉", base: "#F2B7C7" },
  mint: { label: "薄荷绿", base: "#A7DCC8" },
  lavender: { label: "柔雾紫", base: "#CAB9F5" },
  butter: { label: "奶油黄", base: "#F7DE95" },
  coral: { label: "珊瑚橙", base: "#F6B18A" },
  ice: { label: "冰川青", base: "#A9D8E9" },
  rose: { label: "玫灰粉", base: "#D9C3D0" },
  graphite: { label: "石墨灰", base: "#C8CEDA" },
};

export const ACCESSORIES = {
  none: "无装饰",
  sprout: "生命嫩芽",
  halo: "能量环",
  antenna: "智能天线",
  spark: "未来星点",
  beret: "创意贝雷",
  code: "代码模块",
  glasses: "研究眼镜",
  cap: "学习帽",
  leaf: "自然叶片",
};

export const EXPRESSIONS = {
  neutral: { label: "自然", left: "oval", right: "oval", lookX: 0, lookY: 0 },
  happy: { label: "开心", left: "arcUp", right: "arcUp", lookX: 0, lookY: 2 },
  focused: { label: "专注", left: "pill", right: "pill", lookX: 0, lookY: 0 },
  curious: { label: "好奇", left: "dot", right: "oval", lookX: 8, lookY: -3 },
  thinking: {
    label: "思考中",
    left: "pillTilt",
    right: "lineDown",
    lookX: 5,
    lookY: -4,
  },
  sleepy: {
    label: "困倦",
    left: "closed",
    right: "closed",
    lookX: 0,
    lookY: 0,
  },
  surprised: {
    label: "惊讶",
    left: "round",
    right: "round",
    lookX: 0,
    lookY: -1,
  },
  angry: {
    label: "生气",
    left: "lineIn",
    right: "lineInR",
    lookX: 0,
    lookY: 0,
  },
  shy: { label: "害羞", left: "tiny", right: "tiny", lookX: 0, lookY: 1 },
  accept: {
    label: "接收任务",
    left: "pill",
    right: "lineDown",
    lookX: 0,
    lookY: 0,
  },
  relaxed: {
    label: "松弛",
    left: "pillSoft",
    right: "pillSoft",
    lookX: 0,
    lookY: 1,
  },
};

export const STATES = {
  idle: { label: "待机", expression: "neutral", note: "准备好开始" },
  listening: { label: "聆听", expression: "curious", note: "正在认真听你说" },
  thinking: { label: "思考中", expression: "thinking", note: "连接想法与线索" },
  working: { label: "执行中", expression: "focused", note: "一步一步完成任务" },
  success: { label: "已完成", expression: "happy", note: "任务顺利完成" },
  waiting: {
    label: "待确认",
    expression: "accept",
    note: "等待你的下一步决定",
  },
  error: {
    label: "遇到问题",
    expression: "angry",
    note: "出现一点问题，需要处理",
  },
  sleeping: {
    label: "休息中",
    expression: "sleepy",
    note: "稍作休息，随时回来",
  },
};

export const ACTIONS = {
  blink: "眨眼",
  nod: "点头",
  bounce: "弹跳",
  sway: "摇摆",
  spin: "转身",
  celebrate: "庆祝",
};

export type ShapeKey = keyof typeof SHAPES;
export type ColorKey = keyof typeof COLORS;
export type AccessoryKey = keyof typeof ACCESSORIES;
export type StateKey = keyof typeof STATES;
export type ExpressionId = keyof typeof EXPRESSIONS;
export type ExpressionProp = ExpressionId | "auto";
export type ActionKey = keyof typeof ACTIONS;
export type StudioTab = "appearance" | "expressions" | "integration";
export type EyeType =
  | "dot"
  | "tiny"
  | "round"
  | "pill"
  | "pillSoft"
  | "pillTilt"
  | "oval"
  | "closed"
  | "arcUp"
  | "lineDown"
  | "lineIn"
  | "lineInR";

type Theme = ReturnType<typeof makeTheme>;
type ShapeDef = (typeof SHAPES)[ShapeKey];
type AppearanceConfig = {
  shape: ShapeKey;
  color: ColorKey;
  accessory: AccessoryKey;
};

export type AgentAvatarHandle = {
  play: (name: ActionKey) => boolean;
  getSVG: () => SVGSVGElement | null;
  exportSVG: () => string;
};

export type AgentAvatarProps = {
  shape?: ShapeKey;
  color?: ColorKey;
  accessory?: AccessoryKey;
  state?: StateKey;
  expression?: ExpressionProp;
  followMouse?: boolean;
  floatLoop?: boolean;
  animated?: boolean;
  speed?: number;
  intensity?: number;
  compact?: boolean;
  label?: string;
} & Omit<SVGProps<SVGSVGElement>, "color">;

export const PRESETS = [
  {
    id: "general",
    label: "通用伙伴",
    shape: "pebble",
    color: "pearl",
    accessory: "none",
  },
  {
    id: "dev",
    label: "开发伙伴",
    shape: "wedge",
    color: "blue",
    accessory: "code",
  },
  {
    id: "creative",
    label: "创意伙伴",
    shape: "droplet",
    color: "peach",
    accessory: "beret",
  },
  {
    id: "study",
    label: "学习伙伴",
    shape: "kite",
    color: "butter",
    accessory: "cap",
  },
  {
    id: "care",
    label: "陪伴伙伴",
    shape: "puff",
    color: "mint",
    accessory: "sprout",
  },
] as const satisfies ReadonlyArray<{
  id: string;
  label: string;
  shape: ShapeKey;
  color: ColorKey;
  accessory: AccessoryKey;
}>;

function Eye({
  x,
  y,
  type,
  scale = 1,
  color = "#151822",
}: {
  x: number;
  y: number;
  type: EyeType;
  scale?: number;
  color?: string;
}) {
  const s = scale;
  switch (type) {
    case "dot":
      return <circle cx={x} cy={y} r={8 * s} fill={color} />;
    case "tiny":
      return <ellipse cx={x} cy={y} rx={5 * s} ry={8 * s} fill={color} />;
    case "round":
      return <ellipse cx={x} cy={y} rx={10 * s} ry={12 * s} fill={color} />;
    case "pill":
      return (
        <rect
          x={x - 7 * s}
          y={y - 16 * s}
          width={14 * s}
          height={32 * s}
          rx={7 * s}
          fill={color}
        />
      );
    case "pillSoft":
      return (
        <rect
          x={x - 9 * s}
          y={y - 13 * s}
          width={18 * s}
          height={26 * s}
          rx={9 * s}
          fill={color}
        />
      );
    case "pillTilt":
      return (
        <g transform={`translate(${x} ${y}) rotate(20)`}>
          <rect
            x={-7 * s}
            y={-16 * s}
            width={14 * s}
            height={32 * s}
            rx={7 * s}
            fill={color}
          />
        </g>
      );
    case "oval":
      return <ellipse cx={x} cy={y} rx={8 * s} ry={13 * s} fill={color} />;
    case "closed":
      return (
        <path
          d={`M${x - 10 * s} ${y}Q${x} ${y + 3 * s} ${x + 10 * s} ${y}`}
          fill="none"
          stroke={color}
          strokeWidth={6 * s}
          strokeLinecap="round"
        />
      );
    case "arcUp":
      return (
        <path
          d={`M${x - 11 * s} ${y + 2 * s}Q${x} ${y - 9 * s} ${x + 11 * s} ${y + 2 * s}`}
          fill="none"
          stroke={color}
          strokeWidth={6 * s}
          strokeLinecap="round"
        />
      );
    case "lineDown":
      return (
        <path
          d={`M${x - 10 * s} ${y - 4 * s}L${x + 10 * s} ${y + 4 * s}`}
          fill="none"
          stroke={color}
          strokeWidth={7 * s}
          strokeLinecap="round"
        />
      );
    case "lineIn":
      return (
        <path
          d={`M${x - 11 * s} ${y - 3 * s}L${x + 7 * s} ${y + 7 * s}`}
          fill="none"
          stroke={color}
          strokeWidth={7 * s}
          strokeLinecap="round"
        />
      );
    case "lineInR":
      return (
        <path
          d={`M${x - 7 * s} ${y + 7 * s}L${x + 11 * s} ${y - 3 * s}`}
          fill="none"
          stroke={color}
          strokeWidth={7 * s}
          strokeLinecap="round"
        />
      );
    default:
      return <ellipse cx={x} cy={y} rx={8 * s} ry={13 * s} fill={color} />;
  }
}

function Accessory({
  type,
  shape,
  theme,
}: {
  type: AccessoryKey;
  shape: ShapeDef;
  theme: Theme;
}) {
  const [tx, ty] = shape.top as [number, number];
  const accent = theme.accent;
  const soft = theme.soft;
  const line = mix(accent, "#000000", 0.25);
  switch (type) {
    case "sprout":
      return (
        <g transform={`translate(${tx} ${ty})`}>
          <path
            d="M0 2Q4-10 0-21"
            fill="none"
            stroke={line}
            strokeWidth="3.6"
            strokeLinecap="round"
          />
          <path d="M0-15Q-24-39-32-23Q-32-8 0-15" fill={accent} />
          <path d="M1-16Q19-41 31-27Q38-10 1-16" fill={soft} />
        </g>
      );
    case "halo":
      return (
        <ellipse
          cx={tx}
          cy={ty - 12}
          rx="29"
          ry="9"
          fill="none"
          stroke={accent}
          strokeWidth="5"
          opacity=".95"
        />
      );
    case "antenna":
      return (
        <g transform={`translate(${tx} ${ty - 6})`}>
          <path
            d="M0 7V-15"
            stroke={line}
            strokeWidth="4"
            strokeLinecap="round"
          />
          <circle
            cx="0"
            cy="-20"
            r="8"
            fill={accent}
            stroke={soft}
            strokeWidth="2"
          />
        </g>
      );
    case "spark":
      return (
        <g transform={`translate(${tx + 28} ${ty - 10})`}>
          <path d="M0-12 4-4 12 0 4 4 0 12-4 4-12 0-4-4Z" fill={accent} />
        </g>
      );
    case "beret":
      return (
        <g transform={`translate(${tx - 14} ${ty + 3}) rotate(-16)`}>
          <path d="M-38 8Q-43-12-18-20Q8-28 30-13Q46 2 27 11Z" fill={accent} />
          <path
            d="M-26 11Q-3 17 26 10"
            fill="none"
            stroke={line}
            strokeWidth="6"
            strokeLinecap="round"
          />
        </g>
      );
    case "code":
      return (
        <g transform={`translate(${tx + 56} ${ty + 18}) rotate(9)`}>
          <rect
            x="-18"
            y="-15"
            width="36"
            height="29"
            rx="11"
            fill={accent}
            stroke={soft}
            strokeWidth="1.4"
          />
          <path
            d="M-8-6-13-1-8 4M8-6 13-1 8 4M2-9-3 7"
            fill="none"
            stroke="#ffffff"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </g>
      );
    case "glasses": {
      const [lx, ly] = shape.eyes.left as [number, number];
      const [rx, ry] = shape.eyes.right as [number, number];
      return (
        <g fill="none" stroke={accent} strokeWidth="3" strokeLinecap="round">
          <circle cx={lx} cy={ly} r="17" />
          <circle cx={rx} cy={ry} r="17" />
          <path d={`M${lx + 17} ${ly}L${rx - 17} ${ry}`} />
        </g>
      );
    }
    case "cap":
      return (
        <g transform={`translate(${tx} ${ty - 3}) rotate(6)`}>
          <path d="M-23 0V15Q0 27 23 15V0" fill={accent} />
          <path d="M-45-5 0-22 45-5 0 13Z" fill={line} />
        </g>
      );
    case "leaf":
      return (
        <g transform={`translate(${tx} ${ty - 2})`}>
          <path
            d="M0 4q1-10 0-18"
            fill="none"
            stroke={line}
            strokeWidth="3.2"
            strokeLinecap="round"
          />
          <path d="M0-13Q-20-28-28-15Q-28-5 0-13" fill={accent} />
          <path d="M1-14Q17-33 28-20Q32-7 1-14" fill={soft} />
        </g>
      );
    default:
      return null;
  }
}

function CharacterEffects({
  state,
  theme,
  t,
}: {
  state: StateKey;
  theme: Theme;
  t: number;
}) {
  if (state === "thinking") {
    const dots = [0, 1, 2].map((i) => ({
      x: 143 + i * 16,
      y: 100 + Math.sin(t * 3.6 - i * 0.8) * 4,
      o: 0.35 + ((Math.sin(t * 4.6 - i * 0.9) + 1) / 2) * 0.65,
    }));
    return (
      <g>
        {dots.map((d, i) => (
          <circle
            key={i}
            cx={d.x}
            cy={d.y}
            r="3"
            fill={theme.accent}
            opacity={d.o}
          />
        ))}
      </g>
    );
  }
  if (state === "working") {
    const a = (t * 70) % 360;
    return (
      <g transform={`rotate(${a} 160 160)`} opacity=".55">
        <path
          d="M160 96A64 64 0 0 1 224 160"
          fill="none"
          stroke={theme.accent}
          strokeWidth="4"
          strokeLinecap="round"
        />
      </g>
    );
  }
  if (state === "success") {
    const stars = Array.from({ length: 6 }, (_, i) => {
      const ang = (i / 6) * PI * 2 + t * 0.7;
      return {
        x: 160 + Math.cos(ang) * 68,
        y: 160 + Math.sin(ang) * 48,
      };
    });
    return (
      <g opacity=".8">
        {stars.map((s, i) => (
          <path
            key={i}
            transform={`translate(${s.x} ${s.y})`}
            d="M0-7 2-2 7 0 2 2 0 7-2 2-7 0-2-2Z"
            fill={i % 2 ? theme.accent : theme.soft}
          />
        ))}
      </g>
    );
  }
  return null;
}

export const AgentAvatar = forwardRef(function AgentAvatar(
  {
    shape: shapeProp = "pebble",
    color: colorProp = "pearl",
    accessory: accessoryProp = "none",
    state: stateProp = "idle",
    expression: expressionProp = "auto",
    followMouse = true,
    floatLoop = true,
    animated = true,
    speed = 1,
    intensity = 1,
    compact = false,
    label = "智能体头像",
    ...rest
  }: AgentAvatarProps,
  ref: Ref<AgentAvatarHandle>,
) {
  const shape = valid<ShapeKey>(shapeProp, SHAPES, "pebble");
  const color = valid<ColorKey>(colorProp, COLORS, "pearl");
  const accessory = valid<AccessoryKey>(accessoryProp, ACCESSORIES, "none");
  const state = valid<StateKey>(stateProp, STATES, "idle");
  const expressionId: ExpressionId =
    expressionProp === "auto"
      ? (STATES[state].expression as ExpressionId)
      : valid<ExpressionId>(expressionProp, EXPRESSIONS, "neutral");
  const svgRef = useRef<SVGSVGElement>(null);
  const bodyRef = useRef<SVGGElement>(null);
  const eyeWrapRef = useRef<SVGGElement>(null);
  const shadowRef = useRef<SVGEllipseElement>(null);
  const actionRef = useRef<{ name: ActionKey | null; start: number }>({
    name: null,
    start: 0,
  });
  const runtime = useRef({
    t: 0,
    lookX: 0,
    lookY: 0,
    targetX: 0,
    targetY: 0,
    raf: 0,
    blink: 0,
    visible: true,
  });
  const uid = `ally-${useId().replaceAll(":", "")}`;

  const shapeDef = SHAPES[shape];
  const theme = makeTheme(COLORS[color].base);
  const expr = EXPRESSIONS[expressionId];

  useImperativeHandle(
    ref,
    () => ({
      play(name: ActionKey) {
        if (!Object.prototype.hasOwnProperty.call(ACTIONS, name)) return false;
        actionRef.current = { name, start: performance.now() };
        return true;
      },
      getSVG() {
        return svgRef.current;
      },
      exportSVG() {
        const node = svgRef.current;
        if (!node) return "";
        const copy = node.cloneNode(true);
        if (!(copy instanceof SVGSVGElement)) return "";
        copy.setAttribute("xmlns", "http://www.w3.org/2000/svg");
        copy.setAttribute("width", "640");
        copy.setAttribute("height", "640");
        return new XMLSerializer().serializeToString(copy);
      },
    }),
    [],
  );

  useEffect(() => {
    if (!followMouse) return undefined;
    const onMove = (e: PointerEvent) => {
      const bounds = svgRef.current?.getBoundingClientRect();
      if (!bounds) return;
      runtime.current.targetX = clamp(
        (e.clientX - bounds.left - bounds.width / 2) / (bounds.width * 0.35),
        -1,
        1,
      );
      runtime.current.targetY = clamp(
        (e.clientY - bounds.top - bounds.height / 2) / (bounds.height * 0.35),
        -1,
        1,
      );
    };
    const reset = () => {
      runtime.current.targetX = 0;
      runtime.current.targetY = 0;
    };
    window.addEventListener("pointermove", onMove, { passive: true });
    window.addEventListener("pointerleave", reset);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerleave", reset);
    };
  }, [followMouse]);

  useEffect(() => {
    if (!animated) return undefined;
    let frame = 0;
    let last = performance.now();
    const loop = (now: number) => {
      const dt = Math.min((now - last) / 1000, 0.05);
      last = now;
      const rt = runtime.current;
      rt.t += dt * speed;
      rt.lookX += (rt.targetX - rt.lookX) * (1 - Math.exp(-10 * dt));
      rt.lookY += (rt.targetY - rt.lookY) * (1 - Math.exp(-10 * dt));

      let y = floatLoop ? Math.sin(rt.t * 1.8) * 4 * intensity : 0;
      let rotate = floatLoop ? Math.sin(rt.t * 1.1) * 1.8 * intensity : 0;
      let sx = 1;
      let sy = 1 + (floatLoop ? Math.sin(rt.t * 1.7) * 0.018 * intensity : 0);
      let eyeExtraX = 0;
      const eyeExtraY = 0;
      let blink = 1;

      if (state === "thinking") rotate -= 2.5;
      if (state === "working") y -= Math.abs(Math.sin(rt.t * 3.2)) * 2.2;
      if (state === "sleeping") blink = 0.1;

      const active = actionRef.current.name;
      if (active) {
        const progress = clamp(
          (now - actionRef.current.start) /
            (active === "spin" ? 1200 : active === "celebrate" ? 1400 : 900),
          0,
          1,
        );
        switch (active) {
          case "blink":
            blink = 1 - Math.sin(progress * PI);
            break;
          case "nod":
            y += Math.sin(progress * PI * 2) * 10;
            sy -= Math.abs(Math.sin(progress * PI)) * 0.06;
            break;
          case "bounce":
            y -= Math.abs(Math.sin(progress * PI * 2)) * 28;
            sx += Math.sin(progress * PI * 2) * 0.03;
            sy -= Math.sin(progress * PI * 2) * 0.07;
            break;
          case "sway":
            rotate += Math.sin(progress * PI * 4) * 10;
            break;
          case "spin":
            rotate += progress * 360;
            eyeExtraX = Math.sin(progress * PI * 4) * 2;
            break;
          case "celebrate":
            y -= Math.abs(Math.sin(progress * PI * 3)) * 12;
            rotate += Math.sin(progress * PI * 6) * 4;
            break;
          default:
            break;
        }
        if (progress >= 1) actionRef.current = { name: null, start: 0 };
      } else if (animated) {
        const phase = rt.t % 5.5;
        if (phase > 0.02 && phase < 0.16 && state !== "sleeping")
          blink = 1 - Math.sin(((phase - 0.02) / 0.14) * PI);
      }

      bodyRef.current?.setAttribute(
        "transform",
        `translate(160 ${160 + y}) rotate(${rotate}) scale(${sx} ${sy}) translate(-160 -160)`,
      );
      eyeWrapRef.current?.setAttribute(
        "transform",
        `translate(${rt.lookX * 4 + expr.lookX + eyeExtraX} ${rt.lookY * 3 + expr.lookY + eyeExtraY}) scale(1 ${Math.max(blink, 0.06)})`,
      );
      shadowRef.current?.setAttribute(
        "opacity",
        String(clamp(0.18 + y / 150, 0.08, 0.22)),
      );
      shadowRef.current?.setAttribute(
        "transform",
        `translate(160 278) scale(${1 - y / 120} 1) translate(-160 -278)`,
      );

      frame = requestAnimationFrame(loop);
    };
    frame = requestAnimationFrame(loop);
    runtime.current.raf = frame;
    return () => cancelAnimationFrame(frame);
  }, [animated, floatLoop, intensity, speed, state, expr.lookX, expr.lookY]);

  const [leftX = 0, leftY = 0] = shapeDef.eyes.left;
  const [rightX = 0, rightY = 0] = shapeDef.eyes.right;
  const eyeScale = shapeDef.eyes.scale;

  return (
    <svg
      ref={svgRef}
      viewBox={compact ? "40 48 240 240" : "0 0 320 320"}
      role="img"
      aria-label={label}
      style={{
        width: "100%",
        height: "100%",
        display: "block",
        overflow: "visible",
      }}
      data-shape={shape}
      data-color={color}
      data-expression={expressionId}
      id={uid}
      {...rest}
    >
      {!compact && (
        <filter
          id={`${uid}-shadow`}
          x="-40%"
          y="-200%"
          width="180%"
          height="500%"
        >
          <feGaussianBlur stdDeviation="8" />
        </filter>
      )}
      {!compact && (
        <ellipse
          ref={shadowRef}
          cx="160"
          cy="278"
          rx="72"
          ry="9"
          fill={theme.shadow}
          opacity=".16"
          filter={`url(#${uid}-shadow)`}
        />
      )}
      <CharacterEffects state={state} theme={theme} t={0} />
      <g ref={bodyRef}>
        <path
          d={shapeDef.d}
          fill={theme.base}
          stroke={mix(theme.base, "#000000", 0.08)}
          strokeWidth="1.5"
        />
        <Accessory type={accessory} shape={shapeDef} theme={theme} />
        <g ref={eyeWrapRef}>
          <Eye
            x={leftX}
            y={leftY}
            type={expr.left as EyeType}
            scale={eyeScale}
            color={theme.eye}
          />
          <Eye
            x={rightX}
            y={rightY}
            type={expr.right as EyeType}
            scale={eyeScale}
            color={theme.eye}
          />
        </g>
      </g>
    </svg>
  );
});

function TinyButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={`ally-btn ${active ? "active" : ""}`}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function ColorSwatch({
  active,
  color,
  label,
  onClick,
}: {
  active: boolean;
  color: string;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`ally-color ${active ? "active" : ""}`}
      title={label}
      onClick={onClick}
      style={{ ["--sw" as string]: color }}
      aria-label={label}
    />
  );
}

function ShapeCard({
  id,
  active,
  onClick,
}: {
  id: ShapeKey;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`ally-shape-card ${active ? "active" : ""}`}
      onClick={onClick}
    >
      <div className="ally-shape-card-svg">
        <AgentAvatar
          shape={id}
          color="pearl"
          compact
          animated={false}
          followMouse={false}
          floatLoop={false}
        />
      </div>
      <strong>{SHAPES[id].label}</strong>
      <span>{SHAPES[id].desc}</span>
    </button>
  );
}

function ActionButton({
  name,
  onPlay,
}: {
  name: ActionKey;
  onPlay: (name: ActionKey) => void;
}) {
  return (
    <button type="button" className="ally-action" onClick={() => onPlay(name)}>
      {ACTIONS[name]}
    </button>
  );
}

function saveBlob(text: string, filename: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

const DEFAULT_CONFIG: AppearanceConfig = {
  shape: "pebble",
  color: "pearl",
  accessory: "none",
};

export function AllyAvatarStudio() {
  const avatarRef = useRef<AgentAvatarHandle>(null);
  const [tab, setTab] = useState<StudioTab>("appearance");
  const [config, setConfig] = useState<AppearanceConfig>(DEFAULT_CONFIG);
  const [state, setState] = useState<StateKey>("idle");
  const [expression, setExpression] = useState<ExpressionProp>("auto");
  const [animated, setAnimated] = useState(true);
  const [follow, setFollow] = useState(true);
  const [floating, setFloating] = useState(true);
  const [speed, setSpeed] = useState(1);
  const [intensity, setIntensity] = useState(1);
  const [message, setMessage] = useState("");

  const currentPreset = PRESETS.find(
    (p) =>
      p.shape === config.shape &&
      p.color === config.color &&
      p.accessory === config.accessory,
  );
  const displayName = currentPreset?.label || "我的伙伴";

  const play = (name: ActionKey) => {
    avatarRef.current?.play(name);
    setMessage(`已触发动作：${ACTIONS[name]}`);
    setTimeout(() => setMessage(""), 900);
  };

  const exportConfig = () => {
    saveBlob(
      JSON.stringify(
        { version: 10, appearance: config, state, expression },
        null,
        2,
      ),
      "ally-avatar-config.json",
      "application/json",
    );
  };

  const exportSVG = () => {
    const svg = avatarRef.current?.exportSVG?.();
    if (svg) saveBlob(svg, "ally-avatar.svg", "image/svg+xml");
  };

  const currentExpression: ExpressionId =
    expression === "auto"
      ? (STATES[state].expression as ExpressionId)
      : expression;

  return (
    <div className="ally-root">
      <div className="ally-shell">
        <div className="ally-top-row">
          <div className="ally-headline">
            <small>A LITTLE SHAPE. A LOT OF PERSONALITY.</small>
            <h1>每个伙伴，都有自己的样子。</h1>
            <p>
              这版改成更接近「单轮廓 + 眼睛 + 动作」的 IP
              方向：用原创轮廓、眼睛表情和动作状态来表达个性，而不是靠复杂材质和额外面罩。
            </p>
          </div>
          <div className="ally-meta">
            <span>5 种原创形态</span>
            <span>10 种主题</span>
            <span>单色多表情 / 多动作</span>
          </div>
        </div>

        <div className="ally-grid">
          <div>
            <div className="ally-stage">
              <div className="ally-stage-bar">
                <div className="ally-live">
                  <i /> 实时预览{" "}
                  <span
                    style={{
                      border: "1px solid #e8ecf4",
                      borderRadius: 8,
                      padding: "3px 7px",
                      fontSize: 12,
                    }}
                  >
                    LIVE
                  </span>
                </div>
                <div className="ally-toggle-wrap">
                  <button
                    className="ally-btn"
                    type="button"
                    onClick={exportSVG}
                  >
                    导出 SVG
                  </button>
                  <button
                    className="ally-btn"
                    type="button"
                    onClick={exportConfig}
                  >
                    导出配置
                  </button>
                </div>
              </div>

              <div className="ally-stage-main">
                <div className="ally-stage-art">
                  <AgentAvatar
                    ref={avatarRef}
                    shape={config.shape}
                    color={config.color}
                    accessory={config.accessory}
                    state={state}
                    expression={expression}
                    followMouse={follow}
                    floatLoop={floating}
                    animated={animated}
                    speed={speed}
                    intensity={intensity}
                  />
                </div>
                <div className="ally-stage-side">ALLY / IP</div>
                <div className="ally-stage-caption">
                  <h2>{displayName}</h2>
                  <p>
                    {STATES[state].note} · 当前表情：
                    {EXPRESSIONS[currentExpression].label}
                  </p>
                </div>
                {message && <div className="ally-stage-toast">{message}</div>}
              </div>

              <div className="ally-controls-row">
                <div className="ally-toggle-wrap">
                  <label className="ally-switch">
                    <input
                      type="checkbox"
                      checked={follow}
                      onChange={(e) => setFollow(e.target.checked)}
                    />
                    鼠标注视
                  </label>
                  <label className="ally-switch">
                    <input
                      type="checkbox"
                      checked={floating}
                      onChange={(e) => setFloating(e.target.checked)}
                    />
                    呼吸漂浮
                  </label>
                  <label className="ally-switch">
                    <input
                      type="checkbox"
                      checked={animated}
                      onChange={(e) => setAnimated(e.target.checked)}
                    />
                    启用动画
                  </label>
                </div>
                <div className="ally-mini-actions">
                  {(
                    [
                      "blink",
                      "nod",
                      "bounce",
                      "sway",
                      "spin",
                      "celebrate",
                    ] as ActionKey[]
                  ).map((name) => (
                    <button
                      key={name}
                      type="button"
                      className="ally-btn"
                      onClick={() => play(name)}
                    >
                      {ACTIONS[name]}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div className="ally-strip">
              <div>
                <h4>在每个角落，都是它</h4>
                <p>聊天头像 / 成员列表 / 角色卡片 / 状态反馈</p>
              </div>
              <div className="ally-strip-preview">
                <div className="ally-size">
                  <div style={{ width: 32, height: 32 }}>
                    <AgentAvatar
                      shape={config.shape}
                      color={config.color}
                      accessory={config.accessory}
                      state={state}
                      expression={expression}
                      compact
                      animated={false}
                      followMouse={false}
                      floatLoop={false}
                    />
                  </div>
                  <span>32 px</span>
                </div>
                <div className="ally-size">
                  <div style={{ width: 48, height: 48 }}>
                    <AgentAvatar
                      shape={config.shape}
                      color={config.color}
                      accessory={config.accessory}
                      state={state}
                      expression={expression}
                      compact
                      animated={false}
                      followMouse={false}
                      floatLoop={false}
                    />
                  </div>
                  <span>48 px</span>
                </div>
                <div className="ally-size">
                  <div style={{ width: 72, height: 72 }}>
                    <AgentAvatar
                      shape={config.shape}
                      color={config.color}
                      accessory={config.accessory}
                      state={state}
                      expression={expression}
                      compact
                      animated={false}
                      followMouse={false}
                      floatLoop={false}
                    />
                  </div>
                  <span>72 px</span>
                </div>
              </div>
            </div>
          </div>

          <div className="ally-panel">
            <div className="ally-panel-tabs">
              <button
                type="button"
                className={tab === "appearance" ? "active" : ""}
                onClick={() => setTab("appearance" as StudioTab)}
              >
                外观
              </button>
              <button
                type="button"
                className={tab === "expressions" ? "active" : ""}
                onClick={() => setTab("expressions" as StudioTab)}
              >
                表情与动作
              </button>
              <button
                type="button"
                className={tab === "integration" ? "active" : ""}
                onClick={() => setTab("integration" as StudioTab)}
              >
                接入说明
              </button>
            </div>

            <div className="ally-panel-body">
              {tab === "appearance" && (
                <>
                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>角色形态</h3>
                      <span>一个轮廓，就是一种气质</span>
                    </div>
                    <div className="ally-shape-grid">
                      {(Object.keys(SHAPES) as ShapeKey[]).map((id) => (
                        <ShapeCard
                          key={id}
                          id={id}
                          active={config.shape === id}
                          onClick={() =>
                            setConfig((prev) => ({
                              ...prev,
                              shape: id as ShapeKey,
                            }))
                          }
                        />
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>角色主题</h3>
                      <span>主体、装饰统一同色系</span>
                    </div>
                    <div className="ally-color-row">
                      {Object.entries(COLORS).map(([id, def]) => (
                        <ColorSwatch
                          key={id}
                          color={def.base}
                          label={def.label}
                          active={config.color === id}
                          onClick={() =>
                            setConfig((prev) => ({
                              ...prev,
                              color: id as ColorKey,
                            }))
                          }
                        />
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>角色装饰</h3>
                      <span>装饰不抢戏，只做性格点缀</span>
                    </div>
                    <div className="ally-button-row">
                      {Object.entries(ACCESSORIES).map(([id, label]) => (
                        <TinyButton
                          key={id}
                          active={config.accessory === id}
                          onClick={() =>
                            setConfig((prev) => ({
                              ...prev,
                              accessory: id as AccessoryKey,
                            }))
                          }
                        >
                          {label}
                        </TinyButton>
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>快速预设</h3>
                      <span>常用角色一键切换</span>
                    </div>
                    <div className="ally-presets">
                      {PRESETS.map((p) => (
                        <button
                          key={p.id}
                          type="button"
                          onClick={() =>
                            setConfig({
                              shape: p.shape,
                              color: p.color,
                              accessory: p.accessory,
                            })
                          }
                        >
                          {p.label}
                        </button>
                      ))}
                    </div>
                  </section>
                </>
              )}

              {tab === "expressions" && (
                <>
                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>角色状态</h3>
                      <span>状态决定默认表情与氛围</span>
                    </div>
                    <div className="ally-button-row">
                      {Object.entries(STATES).map(([id, def]) => (
                        <TinyButton
                          key={id}
                          active={state === id}
                          onClick={() => setState(id as StateKey)}
                        >
                          {def.label}
                        </TinyButton>
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>表情覆盖</h3>
                      <span>也可以手动指定眼睛表情</span>
                    </div>
                    <div className="ally-button-row">
                      <TinyButton
                        active={expression === "auto"}
                        onClick={() => setExpression("auto")}
                      >
                        跟随状态
                      </TinyButton>
                      {Object.entries(EXPRESSIONS).map(([id, def]) => (
                        <TinyButton
                          key={id}
                          active={expression === id}
                          onClick={() => setExpression(id as ExpressionId)}
                        >
                          {def.label}
                        </TinyButton>
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>动作触发</h3>
                      <span>动作不改变造型，只增强生命感</span>
                    </div>
                    <div className="ally-action-grid">
                      {(Object.keys(ACTIONS) as ActionKey[]).map((name) => (
                        <ActionButton key={name} name={name} onPlay={play} />
                      ))}
                    </div>
                  </section>

                  <section className="ally-section">
                    <div className="ally-section-head">
                      <h3>动效强度</h3>
                      <span>控制速度与幅度</span>
                    </div>
                    <div className="ally-row-2">
                      <div>
                        <div
                          style={{
                            fontSize: 13,
                            color: "#7d869b",
                            marginBottom: 6,
                          }}
                        >
                          动画速度：{speed.toFixed(2)}
                        </div>
                        <input
                          className="ally-slider"
                          type="range"
                          min="0.5"
                          max="1.8"
                          step="0.05"
                          value={speed}
                          onChange={(e) => setSpeed(Number(e.target.value))}
                        />
                      </div>
                      <div>
                        <div
                          style={{
                            fontSize: 13,
                            color: "#7d869b",
                            marginBottom: 6,
                          }}
                        >
                          动作幅度：{intensity.toFixed(2)}
                        </div>
                        <input
                          className="ally-slider"
                          type="range"
                          min="0.6"
                          max="1.8"
                          step="0.05"
                          value={intensity}
                          onChange={(e) => setIntensity(Number(e.target.value))}
                        />
                      </div>
                    </div>
                  </section>
                </>
              )}

              {tab === "integration" && (
                <section className="ally-section" style={{ gap: 14 }}>
                  <div className="ally-section-head">
                    <h3>接入建议</h3>
                    <span>适合做 Agent 头像 / 悬浮助手 / 团队成员形象</span>
                  </div>
                  <div
                    style={{
                      padding: 14,
                      border: "1px solid #e8edf6",
                      borderRadius: 18,
                      background: "#fafcff",
                      color: "#56607a",
                      lineHeight: 1.8,
                      fontSize: 14,
                    }}
                  >
                    <div>
                      1. 角色视觉建议保持 <b>单轮廓 + 眼睛</b>
                      ，不要再叠加复杂材质。
                    </div>
                    <div>
                      2. 状态只需要驱动 <b>眼形、目光、呼吸、摇摆、弹跳</b>{" "}
                      这些最轻量的反馈。
                    </div>
                    <div>
                      3. 形状差异表达“伙伴性格”，装饰只做辅助，不喧宾夺主。
                    </div>
                    <div>
                      4. 如果后续要做表情墙，可以直接按{" "}
                      <b>状态 / 表情 / 形态</b> 三个维度批量渲染。
                    </div>
                  </div>
                  <pre
                    style={{
                      margin: 0,
                      overflow: "auto",
                      padding: 16,
                      borderRadius: 18,
                      background: "#202534",
                      color: "#eff3ff",
                      fontSize: 12.5,
                      lineHeight: 1.7,
                    }}
                  >{`<AgentAvatar
  shape="${config.shape}"
  color="${config.color}"
  accessory="${config.accessory}"
  state="${state}"
  expression="${expression}"
  followMouse={true}
  floatLoop={true}
/>`}</pre>
                </section>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
