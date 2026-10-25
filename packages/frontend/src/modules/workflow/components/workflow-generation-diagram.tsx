import { useId } from "react";
import {
  generationPipelineChannels,
  generationPipelineNodes,
  workflowGenerationLayout,
} from "./workflow-generation-layout";

/** 静态骨架与 Remotion 共用同一横向流水线几何，切换时不改变版式。 */
export function WorkflowGenerationDiagram({
  progress = 0,
  appearances = [1, 1, 1],
  motionEnabled = false,
}: {
  progress?: number;
  appearances?: readonly number[];
  motionEnabled?: boolean;
}) {
  const id = useId().replace(/\W/g, "");
  const layout = workflowGenerationLayout(1120);
  return (
    <svg
      viewBox={`0 0 ${layout.width} ${layout.height}`}
      className="size-full"
      aria-hidden="true"
    >
      <defs>
        <filter id={`${id}-glow`} x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation={3} />
        </filter>
        <linearGradient id={`${id}-scan`}>
          <stop offset="0" stopColor="var(--accent)" stopOpacity={0} />
          <stop offset="0.5" stopColor="var(--accent)" stopOpacity={0.28} />
          <stop offset="1" stopColor="var(--accent)" stopOpacity={0} />
        </linearGradient>
      </defs>
      {layout.phases.map((phase, index) => (
        <g
          key={phase.x}
          data-workflow-loading-phase
          opacity={appearances[index]}
        >
          <rect
            x={phase.x}
            y={phase.y}
            width={layout.phaseWidth}
            height={phase.height}
            className="fill-surface-raised [rx:var(--radius-xl)]"
          />
          <g className="fill-content-secondary" opacity={0.22}>
            {[0, 1, 2].map((mark) => (
              <rect
                key={mark}
                x={phase.x + 16 + mark * 7}
                y={phase.y + 20}
                width={4}
                height={4}
                rx={2}
              />
            ))}
            <rect
              x={phase.x + 16}
              y={phase.y + 44}
              width={184 - index * 16}
              height={10}
              rx={5}
            />
            <rect
              x={phase.x + 16}
              y={phase.y + 70}
              width={layout.phaseWidth - 48}
              height={6}
              rx={3}
              opacity={0.5}
            />
            <rect
              x={phase.x + 16}
              y={phase.y + 85}
              width={layout.phaseWidth - 84}
              height={6}
              rx={3}
              opacity={0.5}
            />
          </g>
        </g>
      ))}
      <rect
        x={416}
        y={244}
        width={288}
        height={188}
        className="fill-card [rx:var(--radius-lg)]"
      />
      <path
        d="M 434 266 l 6 -4 6 4 -6 4 z M 434 272 l 6 4 6 -4"
        className="stroke-content-secondary"
        strokeWidth={1.5}
        fill="none"
        opacity={0.4}
      />
      <rect
        x={456}
        y={262}
        width={136}
        height={7}
        rx={3.5}
        className="fill-content-secondary"
        opacity={0.18}
      />
      {generationPipelineChannels.map((channel, index) => {
        const middle = (channel.x1 + channel.x2) / 2;
        const path = `M ${channel.x1} ${channel.y1} C ${middle} ${channel.y1}, ${middle} ${channel.y2}, ${channel.x2} ${channel.y2}`;
        const travel = (progress * 3 + index * 0.13) % 1;
        return (
          <g key={channel.id}>
            <path
              d={path}
              className="stroke-border-default"
              strokeWidth={1.5}
              fill="none"
            />
            {motionEnabled ? (
              <>
                <path
                  d={path}
                  className="stroke-accent"
                  style={{ strokeWidth: 12 }}
                  fill="none"
                  opacity={0.45}
                  filter={`url(#${id}-glow)`}
                  pathLength={1}
                  strokeDasharray="0.16 0.84"
                  strokeDashoffset={-travel}
                />
                <path
                  d={path}
                  className="stroke-accent"
                  style={{ strokeWidth: 2.5 }}
                  fill="none"
                  opacity={0.9}
                  pathLength={1}
                  strokeDasharray="0.1 0.9"
                  strokeDashoffset={-travel}
                />
                {[0, 0.5].map((offset) => {
                  const point = (travel + offset) % 1;
                  const inverse = 1 - point;
                  const x =
                    inverse ** 3 * channel.x1 +
                    3 * inverse ** 2 * point * middle +
                    3 * inverse * point ** 2 * middle +
                    point ** 3 * channel.x2;
                  const y =
                    inverse ** 3 * channel.y1 +
                    3 * inverse ** 2 * point * channel.y1 +
                    3 * inverse * point ** 2 * channel.y2 +
                    point ** 3 * channel.y2;
                  const opacity = Math.min(1, point * 10, inverse * 10);
                  return (
                    <g key={offset} opacity={opacity}>
                      <circle
                        cx={x}
                        cy={y}
                        r={10}
                        className="fill-accent"
                        opacity={0.22}
                        filter={`url(#${id}-glow)`}
                      />
                      <circle cx={x} cy={y} r={3} className="fill-accent" />
                    </g>
                  );
                })}
              </>
            ) : null}
          </g>
        );
      })}
      {generationPipelineNodes.map((node, index) => {
        const processing = motionEnabled
          ? Math.max(
              0,
              Math.sin((progress * 3 - index * 0.11) * Math.PI * 2),
            ) ** 4
          : 0;
        const scan = (progress * 3 + index * 0.19) % 1;
        const clip = `${id}-${node.id}`;
        return (
          <g key={node.id}>
            <defs>
              <clipPath id={clip}>
                <rect
                  x={node.x}
                  y={node.y}
                  width={node.width}
                  height={node.height}
                  className="[rx:var(--radius-lg)]"
                />
              </clipPath>
            </defs>
            <rect
              x={node.x}
              y={node.y}
              width={node.width}
              height={node.height}
              className={
                node.inset
                  ? "fill-surface-raised [rx:var(--radius-lg)]"
                  : "fill-card [rx:var(--radius-lg)]"
              }
            />
            <rect
              x={node.x}
              y={node.y}
              width={node.width}
              height={node.height}
              className="fill-accent [rx:var(--radius-lg)]"
              opacity={processing * 0.12}
            />
            {motionEnabled ? (
              <>
                <rect
                  x={node.x}
                  y={node.y}
                  width={node.width}
                  height={node.height}
                  className="stroke-accent [rx:var(--radius-lg)]"
                  strokeWidth={1.5}
                  fill="none"
                  opacity={processing * 0.8}
                />
                <rect
                  x={node.x - node.width / 2 + scan * node.width * 1.5}
                  y={node.y}
                  width={node.width / 2}
                  height={node.height}
                  fill={`url(#${id}-scan)`}
                  clipPath={`url(#${clip})`}
                />
              </>
            ) : null}
            <g
              className="fill-content-primary"
              opacity={0.16 + processing * 0.08}
            >
              <rect
                x={node.x + 16}
                y={node.y + 16}
                width={node.width * (index % 2 ? 0.64 : 0.76)}
                height={8}
                rx={4}
              />
              <rect
                x={node.x + 36}
                y={node.y + 35}
                width={node.width * 0.36}
                height={5}
                rx={2.5}
                opacity={0.7}
              />
            </g>
            <path
              d={`M ${node.x + 21} ${node.y + 33} l -4 4 4 4 M ${node.x + 27} ${node.y + 33} l 4 4 -4 4`}
              className="stroke-content-secondary"
              fill="none"
              strokeWidth={1.5}
              opacity={0.35}
            />
            <circle
              cx={node.x + node.width - 16}
              cy={node.y + node.height - 13}
              r={2.5}
              className="fill-accent"
              opacity={0.15 + processing * 0.65}
            />
          </g>
        );
      })}
    </svg>
  );
}
