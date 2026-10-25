import { Easing, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { WorkflowGenerationDiagram } from "./workflow-generation-diagram";

/** 帧只驱动装饰性的流动与处理脉冲，完成状态仍由真实版本回执决定。 */
export function WorkflowGenerationComposition({
  motionEnabled = true,
  ease,
  enterFrames,
}: {
  motionEnabled?: boolean;
  ease: [number, number, number, number] | undefined;
  enterFrames: number;
}) {
  const frame = useCurrentFrame();
  const { durationInFrames } = useVideoConfig();
  return (
    <WorkflowGenerationDiagram
      progress={
        motionEnabled ? interpolate(frame, [0, durationInFrames], [0, 1]) : 0
      }
      appearances={[0, 1, 2].map((index) =>
        motionEnabled
          ? interpolate(
              frame,
              [index * enterFrames, (index + 1) * enterFrames],
              [0.9, 1],
              {
                extrapolateLeft: "clamp",
                extrapolateRight: "clamp",
                easing: ease ? Easing.bezier(...ease) : undefined,
              },
            )
          : 1,
      )}
      motionEnabled={motionEnabled}
    />
  );
}
