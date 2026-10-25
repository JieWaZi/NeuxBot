import { useImperativeHandle, useLayoutEffect, type Ref } from "react";
import { vi } from "vitest";
import type { ForceGraphMethods, ForceGraphProps } from "react-force-graph-2d";
import type { ForceGraphLink, ForceGraphNode } from "./knowledge-force-data";

type Props = ForceGraphProps<ForceGraphNode, ForceGraphLink>;
type Methods = ForceGraphMethods<ForceGraphNode, ForceGraphLink>;
const center = { x: 0, y: 0 };
let scale = 1;
const methods = {
  pauseAnimation: vi.fn(),
  resumeAnimation: vi.fn(),
  d3Force: vi.fn(),
  d3ReheatSimulation: vi.fn(),
  centerAt: vi.fn((x?: number, y?: number) => {
    if (x !== undefined) center.x = x;
    if (y !== undefined) center.y = y;
    return center;
  }),
  zoom: vi.fn((value?: number) => {
    if (value !== undefined) scale = value;
    return scale;
  }),
  zoomToFit: vi.fn(),
  screen2GraphCoords: (x: number, y: number) => ({ x, y }),
  graph2ScreenCoords: (x: number, y: number) => ({ x, y }),
  getGraphBbox: vi.fn(),
  emitParticle: vi.fn(),
};
export const graphEngine: { props?: Props; methods: typeof methods } = {
  methods,
};
/** 测试只替代浏览器 Canvas 与物理时钟，业务事件仍进入真实画布适配。 */
export default function ForceGraphDouble({
  ref,
  ...props
}: Props & { ref?: Ref<Methods> }) {
  useLayoutEffect(() => {
    graphEngine.props = props;
  });
  useImperativeHandle(ref, () => methods as unknown as Methods, []);
  return <canvas data-testid="force-canvas" />;
}
