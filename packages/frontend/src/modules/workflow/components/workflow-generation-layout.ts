/** 编排示意保持真实画布的横向轮廓，窄画布整体缩放，不堆成纵向加载列表。 */
export function workflowGenerationLayout(
  availableWidth: number,
  availableHeight = Infinity,
) {
  return {
    width: 1120,
    height: 600,
    displayWidth: Math.max(
      1,
      Math.min(1120, availableWidth, (availableHeight * 1120) / 600),
    ),
    phaseWidth: 320,
    phases: [0, 1, 2].map((index) => ({
      x: 20 + index * 380,
      y: 40,
      height: [320, 520, 240][index] ?? 320,
    })),
  };
}
export const workflowGenerationTiming = { fps: 30, durationInFrames: 180 };

/** 节点只表达编排结构，不冒充实际任务、结果或执行进度。 */
export const generationPipelineNodes = [
  { id: "input", x: 36, y: 148, width: 288, height: 72, inset: false },
  { id: "prepare", x: 36, y: 244, width: 288, height: 72, inset: false },
  { id: "dispatch", x: 416, y: 148, width: 288, height: 72, inset: false },
  { id: "process", x: 432, y: 296, width: 256, height: 48, inset: true },
  { id: "merge", x: 432, y: 364, width: 256, height: 48, inset: true },
  { id: "assemble", x: 416, y: 456, width: 288, height: 72, inset: false },
  { id: "output", x: 796, y: 148, width: 288, height: 72, inset: false },
];
export const generationPipelineChannels = [
  { id: "stage-one-two", x1: 340, y1: 100, x2: 400, y2: 100 },
  { id: "stage-two-three", x1: 720, y1: 100, x2: 780, y2: 100 },
  { id: "prepare", x1: 180, y1: 220, x2: 180, y2: 244 },
  { id: "dispatch", x1: 324, y1: 280, x2: 416, y2: 184 },
  { id: "process", x1: 560, y1: 220, x2: 560, y2: 296 },
  { id: "merge", x1: 560, y1: 344, x2: 560, y2: 364 },
  { id: "assemble", x1: 560, y1: 412, x2: 560, y2: 456 },
  { id: "output", x1: 704, y1: 492, x2: 796, y2: 184 },
];
