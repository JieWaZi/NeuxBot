import { readFileSync, writeFileSync } from "node:fs";

const path =
  "packages/frontend/src/modules/waker/components/avatar-studio/ally-avatar-studio.tsx";
let text = readFileSync(path, "utf8");

const header = `import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ChangeEvent,
  type ComponentPropsWithoutRef,
  type ReactNode,
  type Ref,
  type SVGProps,
} from "react";

import "./ally-avatar-studio.css";

`;

text = text.replace(/import React, \{[\s\S]*?\} from 'react';\n\n/, header);

text = text.replace(
  /\/\/ v4 IP Theme:[\s\S]*?function deriveTheme\(base, accent\) \{[\s\S]*?\}\n\n/,
  "",
);

const typeBlock = `
export type ShapeKey = keyof typeof SHAPES;
export type ColorKey = keyof typeof COLORS;
export type AccessoryKey = keyof typeof ACCESSORIES;
export type StateKey = keyof typeof STATES;
export type ExpressionKey = keyof typeof EXPRESSIONS;
export type MaterialKey = keyof typeof MATERIALS;
export type ActionKey = keyof typeof ACTIONS;
export type StageBackground = "light" | "lavender" | "dark";
export type StudioTab = "appearance" | "animation" | "code";

type ShapeDef = (typeof SHAPES)[ShapeKey] & {
  face: [number, number, number, number, number];
  top: [number, number];
};
type Point = [number, number];
type FrameCallback = (dt: number) => void;
type EyeSpec = [string, number, number, number];
type ContourMap = Record<string, Point[]>;
type RuntimeProps = {
  state: StateKey;
  effectiveExpression: string;
  animated: boolean;
  followMouse: boolean;
  floatLoop: boolean;
  speed: number;
  intensity: number;
  onActionChange?: (name: ActionKey | null) => void;
  reducedMotion: boolean;
};
type RuntimeAction = { name: ActionKey; elapsed: number };
type RuntimeState = {
  time: number;
  blinkTime: number;
  blink: number;
  action: RuntimeAction | null;
  lookX: number;
  lookY: number;
  targetX: number;
  targetY: number;
  eyes: Point[][];
};
type AppearanceConfig = {
  shape: ShapeKey;
  color: ColorKey;
  accessory: AccessoryKey;
  material: MaterialKey;
  mouth: boolean;
};

export type AgentAvatarHandle = {
  play: (name: ActionKey) => boolean;
  stop: () => void;
  getSVG: () => SVGSVGElement | null;
  exportSVG: () => string;
};

export type AgentAvatarProps = {
  shape?: ShapeKey;
  color?: ColorKey;
  accessory?: AccessoryKey;
  state?: StateKey;
  expression?: ExpressionKey;
  material?: MaterialKey;
  animated?: boolean;
  followMouse?: boolean;
  floatLoop?: boolean;
  mouth?: boolean;
  compact?: boolean;
  speed?: number;
  intensity?: number;
  label?: string;
  onActionChange?: (name: ActionKey | null) => void;
} & Omit<SVGProps<SVGSVGElement>, "color">;
`;

if (!text.includes("export type ShapeKey")) {
  text = text.replace(
    "export const PRESETS = [",
    `${typeBlock}\nexport const PRESETS = [`,
  );
}

text = text.replace(
  /export const PRESETS = \[[\s\S]*?\];/,
  (block) =>
    `${block.trim().replace(/;\s*$/, "").replace(/'/g, '"')} as const satisfies ReadonlyArray<{
  id: string;
  label: string;
  shape: ShapeKey;
  color: ColorKey;
  accessory: AccessoryKey;
}>;`,
);

const replacements = [
  [
    "const MATERIALS = { clay: 'IP单色', flat: '纯色', outline: '线稿' };",
    'const MATERIALS = { clay: "IP单色", flat: "纯色", outline: "线稿" } as const;',
  ],
  [
    "const clamp = (x, lo, hi) => Math.max(lo, Math.min(hi, x));",
    "const clamp = (x: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, x));",
  ],
  [
    "const randomKey = (obj) => Object.keys(obj)[Math.floor(Math.random() * Object.keys(obj).length)];",
    "const randomKey = <T extends string>(obj: Record<T, unknown>) => { const keys = Object.keys(obj) as T[]; return keys[Math.floor(Math.random() * keys.length)]!; };",
  ],
  [
    "const valid = (value, catalog, fallback) => Object.hasOwn(catalog, value) ? value : fallback;",
    "const valid = <T extends string>(value: string, catalog: Record<T, unknown>, fallback: T) => (Object.hasOwn(catalog, value) ? (value as T) : fallback);",
  ],
  [
    "function mix(a, b, t) {",
    "function mix(a: string, b: string, t: number) {",
  ],
  [
    "const clients = new Set(); let frame = 0; let last = 0;",
    "const clients = new Set<FrameCallback>(); let frame = 0; let last = 0;",
  ],
  ["const tick = time => {", "const tick = (time: number) => {"],
  ["add(fn) {", "add(fn: FrameCallback) {"],
  [
    "function samplePolygon(vertices) {",
    "function samplePolygon(vertices: Point[]) {",
  ],
  ["function contour(kind) {", "function contour(kind: string) {"],
  [
    "const CONTOURS = Object.fromEntries(['pill','ellipse','arc','down','star','heart','cross','chevron'].map(k => [k, contour(k)]));",
    "const CONTOURS = Object.fromEntries(['pill','ellipse','arc','down','star','heart','cross','chevron'].map(k => [k, contour(k)])) as ContourMap;",
  ],
  ["const EYE_SPEC = {", "const EYE_SPEC: Record<string, EyeSpec[]> = {"],
  ["function pointsPath(points) {", "function pointsPath(points: Point[]) {"],
  [
    "function expressionFor(state, expression) {",
    "function expressionFor(state: StateKey, expression: ExpressionKey) {",
  ],
  [
    "function Accessory({ type, shape, accent, base }) {",
    "function Accessory({ type, shape, accent }: { type: AccessoryKey; shape: ShapeDef; accent: string; base: string }) {",
  ],
  [
    "const common = { strokeLinecap: 'round', strokeLinejoin: 'round' };",
    `const common: Pick<SVGProps<SVGPathElement>, "strokeLinecap" | "strokeLinejoin"> = {
    strokeLinecap: "round",
    strokeLinejoin: "round",
  };`,
  ],
  [
    "function StatusMark({ state, accent }) {",
    "function StatusMark({ state, accent }: { state: StateKey; accent: string }) {",
  ],
  [
    "  const props = { fill: 'none', stroke: accent, strokeWidth: 2.8, strokeLinecap: 'round', strokeLinejoin: 'round' };",
    '  const props = { fill: "none", stroke: accent, strokeWidth: 2.8, strokeLinecap: "round", strokeLinejoin: "round" } as const;',
  ],
  ["const move = e => {", "const move = (e: PointerEvent) => {"],
  [
    "const draw = (dt, snap = false) => {",
    "const draw = (dt: number, snap = false) => {",
  ],
  [", twice = Math.sin(progress * PI * 2)", ""],
  [
    "const drawRef = useRef(draw); drawRef.current = draw;",
    "const drawRef = useRef(draw);\n  useLayoutEffect(() => {\n    drawRef.current = draw;\n  });",
  ],
  [
    "export default function AllyAvatarStudio() {",
    `function readStoredAppearance(): AppearanceConfig {
  try {
    const stored = localStorage.getItem("ally-avatar-studio-v3");
    if (stored) return safeAppearance(JSON.parse(stored));
  } catch {
    // 禁用存储也可以正常使用。
  }
  return DEFAULT_CONFIG;
}

export function AllyAvatarStudio() {`,
  ],
  ["    <style>{STUDIO_CSS}</style>\n", ""],
  [
    "<span>单色多表情</div></section>",
    "<span>单色多表情</span></div></section>",
  ],
  [
    "function saveBlob(text, filename, type) {",
    "function saveBlob(text: string, filename: string, type: string) {",
  ],
  ["const play = name => {", "const play = (name: ActionKey) => {"],
  [
    "const importConfig = async e => {",
    "const importConfig = async (e: ChangeEvent<HTMLInputElement>) => {",
  ],
  [
    "onClick={() => setStageBackground(v)}",
    "onClick={() => setStageBackground(v as StageBackground)}",
  ],
  ["onClick={() => setTab(id)}", "onClick={() => setTab(id as StudioTab)}"],
  [
    "onClick={() => set({ shape: key })}",
    "onClick={() => set({ shape: key as ShapeKey })}",
  ],
  [
    "onClick={() => set({ color: key })}",
    "onClick={() => set({ color: key as ColorKey })}",
  ],
  [
    "onClick={() => set({ material: key })}",
    "onClick={() => set({ material: key as MaterialKey })}",
  ],
  [
    "onClick={() => set({ accessory: key })}",
    "onClick={() => set({ accessory: key as AccessoryKey })}",
  ],
  [
    "onClick={() => { setAutoPlay(false); setState(key); }}",
    "onClick={() => { setAutoPlay(false); setState(key as StateKey); }}",
  ],
  [
    "onClick={() => { setAutoPlay(false); setExpression(key); }}",
    "onClick={() => { setAutoPlay(false); setExpression(key as ExpressionKey); }}",
  ],
  ["onClick={() => play(key)}", "onClick={() => play(key as ActionKey)}"],
  [
    "onClick={() => { setExpression(key); setTab('animation'); }}",
    "onClick={() => { setExpression(key as ExpressionKey); setTab('animation'); }}",
  ],
  [
    "style={{ '--swatch': value.base }}",
    "style={{ ['--swatch' as string]: value.base }}",
  ],
  [
    "shape={key} color={config.color}",
    "shape={key as ShapeKey} color={config.color}",
  ],
  [
    "expression={key} animated={false}",
    "expression={key as ExpressionKey} animated={false}",
  ],
  ["ACTIONS[activeAction][0] + '…'", "`${ACTIONS[activeAction][0]}…`"],
];

for (const [from, to] of replacements) {
  if (!text.includes(from)) {
    console.warn("missing:", from.slice(0, 60));
  }
  text = text.replace(from, to);
}

text = text.replace(
  `function eyeTarget(spec) {
  const [kind, w, h, degrees] = spec, a = degrees * PI / 180;
  return CONTOURS[kind].map(([x, y]) => [x * w * Math.cos(a) - y * h * Math.sin(a), x * w * Math.sin(a) + y * h * Math.cos(a)]);
}
const TARGETS = Object.fromEntries(Object.entries(EYE_SPEC).map(([k, specs]) => [k, specs.map(eyeTarget)]));`,
  `function eyeTarget(spec: EyeSpec) {
  const [kind, w, h, degrees] = spec;
  const a = (degrees * PI) / 180;
  return (CONTOURS[kind] ?? CONTOURS.pill!).map(
    ([x, y]) => [x * w * Math.cos(a) - y * h * Math.sin(a), x * w * Math.sin(a) + y * h * Math.cos(a)] as Point,
  );
}
const TARGETS = Object.fromEntries(Object.entries(EYE_SPEC).map(([k, specs]) => [k, specs.map(eyeTarget)])) as Record<string, Point[][]>;`,
);

text = text.replace(
  `function samplePolygon(vertices: Point[]) {
  const lengths = vertices.map((p, i) => Math.hypot(p[0] - vertices[(i + 1) % vertices.length][0], p[1] - vertices[(i + 1) % vertices.length][1]));
  const total = lengths.reduce((a, b) => a + b, 0);
  return Array.from({ length: N }, (_, i) => {
    let d = i * total / N, j = 0;
    while (d > lengths[j] && j < vertices.length - 1) d -= lengths[j++];
    const a = vertices[j], b = vertices[(j + 1) % vertices.length], t = d / lengths[j];
    return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
  });
}`,
  `function samplePolygon(vertices: Point[]) {
  const lengths = vertices.map((p, i) => {
    const next = vertices[(i + 1) % vertices.length]!;
    return Math.hypot(p[0] - next[0], p[1] - next[1]);
  });
  const total = lengths.reduce((a, b) => a + b, 0);
  return Array.from({ length: N }, (_, i) => {
    let d = (i * total) / N;
    let j = 0;
    while (d > lengths[j]! && j < vertices.length - 1) d -= lengths[j++]!;
    const a = vertices[j]!;
    const b = vertices[(j + 1) % vertices.length]!;
    const t = d / lengths[j]!;
    return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t] as Point;
  });
}`,
);

text = text.replace(
  /\/\*\*\r?\n \* @typedef \{Object\} AgentAvatarProps[\s\S]*?\*\/\nexport const AgentAvatar = forwardRef\(function AgentAvatar\(\{[\s\S]*?\}, forwardedRef\) \{[\s\S]*?const isOutline = material === 'outline';/,
  `export const AgentAvatar = forwardRef(function AgentAvatar(
  {
    shape: shapeProp = "pebble",
    color: colorProp = "pearl",
    accessory: accessoryProp = "none",
    state: stateProp = "idle",
    expression: expressionProp = "auto",
    material: materialProp = "clay",
    animated = true,
    followMouse = false,
    floatLoop = true,
    mouth = false,
    compact = false,
    speed: speedProp = 1,
    intensity: intensityProp = 1,
    label = "AI 伙伴头像",
    onActionChange,
    ...rest
  }: AgentAvatarProps,
  forwardedRef: Ref<AgentAvatarHandle>,
) {
  const shape = valid<ShapeKey>(shapeProp, SHAPES, "pebble");
  const color = valid<ColorKey>(colorProp, COLORS, "pearl");
  const state = valid<StateKey>(stateProp, STATES, "idle");
  const expression = valid<ExpressionKey>(expressionProp, EXPRESSIONS, "auto");
  const accessory = valid<AccessoryKey>(accessoryProp, ACCESSORIES, "none");
  const material = valid<MaterialKey>(materialProp, MATERIALS, "clay");
  const speed = Number.isFinite(speedProp) ? clamp(speedProp, 0.25, 2) : 1;
  const intensity = Number.isFinite(intensityProp) ? clamp(intensityProp, 0, 1.8) : 1;
  const uid = "ally" + useId().replace(/[^a-zA-Z0-9]/g, "");
  const svgRef = useRef<SVGSVGElement>(null);
  const bodyRef = useRef<SVGGElement>(null);
  const shadowRef = useRef<SVGEllipseElement>(null);
  const faceMotionRef = useRef<SVGGElement>(null);
  const leftEyeRef = useRef<SVGPathElement>(null);
  const rightEyeRef = useRef<SVGPathElement>(null);
  const eyeGroupRef = useRef<SVGGElement>(null);
  const statusRef = useRef<SVGGElement>(null);
  const armRef = useRef<SVGGElement>(null);
  const confettiRef = useRef<SVGGElement>(null);
  const [visible, setVisible] = useState(true);
  const [reducedMotion, setReducedMotion] = useState(false);
  const effectiveExpression = expressionFor(state, expression);
  const runtime = useRef<RuntimeState>({
    time: 0,
    blinkTime: 2 + Math.random() * 2,
    blink: 1,
    action: null,
    lookX: 0,
    lookY: 0,
    targetX: 0,
    targetY: 0,
    eyes: (TARGETS[effectiveExpression] ?? TARGETS.neutral)!.map((eye) =>
      eye.map((point) => [...point] as Point),
    ),
  });
  const props = useRef<RuntimeProps>({
    state,
    effectiveExpression,
    animated,
    followMouse,
    floatLoop,
    speed,
    intensity,
    onActionChange,
    reducedMotion,
  });
  props.current = {
    state,
    effectiveExpression,
    animated,
    followMouse,
    floatLoop,
    speed,
    intensity,
    onActionChange,
    reducedMotion,
  };
  const form = SHAPES[shape] as ShapeDef;
  const palette = COLORS[color];
  const [fx, fy, fw, fh, fr] = form.face;
  const eyeY = fy + fh * 0.49;
  const base = palette.base;
  const accent = palette.accent;
  const eyeFill = effectiveExpression === 'love' ? '#ffd5e5' : effectiveExpression === 'stars' ? '#fff2b1' : '#f9fbff';
  const isOutline = material === 'outline';`,
);

text = text.replace(
  `const observer = typeof IntersectionObserver !== 'undefined' ? new IntersectionObserver(entries => setVisible(entries[0].isIntersecting)) : null;
    observer?.observe(node);`,
  `let observer: IntersectionObserver | null = null;
    if (typeof IntersectionObserver !== "undefined" && node) {
      observer = new IntersectionObserver((entries) =>
        setVisible(entries[0]?.isIntersecting ?? false),
      );
      observer.observe(node);
    }`,
);

text = text.replace(
  `const target = TARGETS[p.effectiveExpression];
    for (let side = 0; side < 2; side++) for (let i = 0; i < N; i++) {
      r.eyes[side][i][0] += (target[side][i][0] - r.eyes[side][i][0]) * smoothing;
      r.eyes[side][i][1] += (target[side][i][1] - r.eyes[side][i][1]) * smoothing;
    }`,
  `const target = TARGETS[p.effectiveExpression] ?? TARGETS.neutral!;
    for (let side = 0; side < 2; side++) {
      const eyeTargetPoints = target[side]!;
      const eyeRuntimePoints = r.eyes[side]!;
      for (let i = 0; i < N; i++) {
        const targetPoint = eyeTargetPoints[i]!;
        const runtimePoint = eyeRuntimePoints[i]!;
        runtimePoint[0] += (targetPoint[0] - runtimePoint[0]) * smoothing;
        runtimePoint[1] += (targetPoint[1] - runtimePoint[1]) * smoothing;
      }
    }`,
);

text = text.replace(
  "const progress = clamp(action.elapsed / (ACTIONS[action.name][1] / 1000), 0, 1);",
  "const progress = clamp(action.elapsed / ((ACTIONS[action.name][1] as number) / 1000), 0, 1);",
);

text = text.replace(
  "leftEyeRef.current?.setAttribute('d', pointsPath(r.eyes[0]));\n    rightEyeRef.current?.setAttribute('d', pointsPath(r.eyes[1]));",
  'leftEyeRef.current?.setAttribute("d", pointsPath(r.eyes[0]!));\n    rightEyeRef.current?.setAttribute("d", pointsPath(r.eyes[1]!));',
);

text = text.replace(
  "d={pointsPath(TARGETS[effectiveExpression][0])}/>\n          <path ref={rightEyeRef} transform={`translate(188 ${eyeY})`} d={pointsPath(TARGETS[effectiveExpression][1])}",
  "d={pointsPath((TARGETS[effectiveExpression] ?? TARGETS.neutral)![0]!)}/>\n          <path ref={rightEyeRef} transform={`translate(188 ${eyeY})`} d={pointsPath((TARGETS[effectiveExpression] ?? TARGETS.neutral)![1]!)}",
);

text = text.replace(
  `exportSVG() {
      const copy = svgRef.current.cloneNode(true);
      copy.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
      copy.setAttribute('width', '640'); copy.setAttribute('height', '640');
      return new XMLSerializer().serializeToString(copy);
    }`,
  `exportSVG() {
      const node = svgRef.current;
      if (!node) return "";
      const copy = node.cloneNode(true);
      if (!(copy instanceof SVGSVGElement)) return "";
      copy.setAttribute("xmlns", "http://www.w3.org/2000/svg");
      copy.setAttribute("width", "640");
      copy.setAttribute("height", "640");
      return new XMLSerializer().serializeToString(copy);
    }`,
);

text = text.replace(
  /function Icon\(\{ name, size = 18, \.\.\.props \}\) \{[\s\S]*?\n\}\nfunction Switch\(\{ checked, onChange, children \}\) \{[\s\S]*?\n\}/,
  `const iconPaths = {
  shuffle: "M3 5h3l12 14h3M17 2l4 3-4 3M3 19h3l5-6M14 9l4-4h3M17 16l4 3-4 3",
  reset: "M3 10a9 9 0 1 1 2 8M3 4v6h6",
  download: "M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5",
  copy: "M9 9h11v11H9zM15 5V3H3v12h2",
  play: "M7 4l14 8-14 8z",
  pause: "M8 5v14M16 5v14",
  code: "M8 6l-6 6 6 6m8-12 6 6-6 6M14 3l-4 18",
  check: "M4 12l5 5L20 6",
  sparkle: "M12 2l3 7 7 3-7 3-3 7-3-7-7-3 7-3z",
  glasses: "M10 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0zm12 0a4 4 0 1 1-8 0 4 4 0 0 1 8 0zM10 11h4",
  chart: "M5 20v-7m7 7V4m7 16V9",
  cap: "M1 9l11-5 11 5-11 5zM6 12v6q6 4 12 0v-6M22 10v9",
  beret: "M4 16Q-1 5 11 5q16-1 9 11M4 17q8 5 16-1M12 5V2",
  sprout: "M12 22V11M12 13Q-1 14 3 4q10 0 9 9M12 11Q12 1 22 2q3 10-10 9",
  pencil: "M3 17L17 3l4 4L7 21H3zM14 6l4 4",
  none: "M12 2a10 10 0 1 0 0 20 10 10 0 1 0 0-20M5 5l14 14",
  sliders: "M3 6h6m6 0h6M3 18h12m6 0h-1M12 3v6M18 15v6",
} as const;

function Icon({
  name,
  size = 18,
  ...props
}: { name: string; size?: number } & ComponentPropsWithoutRef<"svg">) {
  const path = name in iconPaths ? iconPaths[name as keyof typeof iconPaths] : iconPaths.sparkle;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
      <path d={path} />
    </svg>
  );
}
function Switch({ checked, onChange, children }: { checked: boolean; onChange: (value: boolean) => void; children: ReactNode }) {
  return (
    <button type="button" className="as-switch-label" role="switch" aria-checked={checked} onClick={() => onChange(!checked)}>
      <span className={\`as-switch \${checked ? "on" : ""}\`}><i /></span>
      {children}
    </button>
  );
}`,
);

text = text.replace(
  "const DEFAULT_CONFIG = { shape: 'pebble', color: 'pearl', accessory: 'none', material: 'flat', mouth: false };",
  'const DEFAULT_CONFIG: AppearanceConfig = { shape: "pebble", color: "pearl", accessory: "none", material: "flat", mouth: false };',
);

text = text.replace(
  `function safeAppearance(input) {
  if (!input || typeof input !== 'object') return { ...DEFAULT_CONFIG };
  return { shape: valid(input.shape, SHAPES, 'pebble'), color: valid(input.color, COLORS, 'pearl'), accessory: valid(input.accessory, ACCESSORIES, 'none'), material: valid(input.material, MATERIALS, 'flat'), mouth: input.mouth === true };
}`,
  `function safeAppearance(input: unknown): AppearanceConfig {
  if (!input || typeof input !== "object") return { ...DEFAULT_CONFIG };
  const appearance = input as Partial<AppearanceConfig>;
  return {
    shape: valid<ShapeKey>(String(appearance.shape ?? ""), SHAPES, "pebble"),
    color: valid<ColorKey>(String(appearance.color ?? ""), COLORS, "pearl"),
    accessory: valid<AccessoryKey>(String(appearance.accessory ?? ""), ACCESSORIES, "none"),
    material: valid<MaterialKey>(String(appearance.material ?? ""), MATERIALS, "flat"),
    mouth: appearance.mouth === true,
  };
}`,
);

text = text.replace(
  "const [config, setConfig] = useState(DEFAULT_CONFIG), [tab, setTab] = useState('appearance');\n  const [state, setState] = useState('idle'), [expression, setExpression] = useState('auto');",
  'const [config, setConfig] = useState(readStoredAppearance);\n  const [tab, setTab] = useState<StudioTab>("appearance");\n  const [state, setState] = useState<StateKey>("idle");\n  const [expression, setExpression] = useState<ExpressionKey>("auto");',
);

text = text.replace(
  "const [stageBackground, setStageBackground] = useState('light'), [activeAction, setActiveAction] = useState(null);",
  'const [stageBackground, setStageBackground] = useState<StageBackground>("light");\n  const [activeAction, setActiveAction] = useState<ActionKey | null>(null);',
);

text = text.replace(
  "const avatarRef = useRef(null), fileRef = useRef(null);\n  const [hydrated, setHydrated] = useState(false);\n  const set = patch => setConfig(prev => ({ ...prev, ...patch }));",
  "const avatarRef = useRef<AgentAvatarHandle>(null);\n  const fileRef = useRef<HTMLInputElement>(null);\n  const set = (patch: Partial<AppearanceConfig>) => setConfig((prev) => ({ ...prev, ...patch }));",
);

text = text.replace(
  `useEffect(() => {
    try { const stored = localStorage.getItem('ally-avatar-studio-v3'); if (stored) setConfig(safeAppearance(JSON.parse(stored))); } catch { /* 禁用存储也可以正常使用。 */ }
    setHydrated(true);
  }, []);
  useEffect(() => { if (hydrated) { try { localStorage.setItem('ally-avatar-studio-v3', JSON.stringify(config)); } catch {} } }, [config, hydrated]);`,
  `useEffect(() => {
    try {
      localStorage.setItem("ally-avatar-studio-v3", JSON.stringify(config));
    } catch {
      // 禁用存储也可以正常使用。
    }
  }, [config]);`,
);

text = text.replace(
  `const sequence = [['listening','wave'],['thinking','nod'],['searching','shake'],['working','stretch'],['waiting','blink'],['success','celebrate']];
    const advance = () => { const [next, action] = sequence[index++ % sequence.length]; setState(next); setExpression('auto'); avatarRef.current?.play(action); };
    advance(); const id = setInterval(advance, 2800); return () => clearInterval(id);`,
  `const sequence: Array<[StateKey, ActionKey]> = [
      ["listening", "wave"],
      ["thinking", "nod"],
      ["searching", "shake"],
      ["working", "stretch"],
      ["waiting", "blink"],
      ["success", "celebrate"],
    ];
    const advance = () => {
      const [next, action] = sequence[index++ % sequence.length]!;
      setState(next);
      setExpression("auto");
      avatarRef.current?.play(action);
    };
    advance();
    const id = window.setInterval(advance, 2800);
    return () => window.clearInterval(id);`,
);

if (!text.includes("export const SHAPES")) {
  throw new Error("SHAPES missing after transform");
}
if (!text.includes("export const AgentAvatar")) {
  throw new Error("AgentAvatar missing after transform");
}

writeFileSync(path, text);
console.log("transformed", text.split("\n").length, "lines");
