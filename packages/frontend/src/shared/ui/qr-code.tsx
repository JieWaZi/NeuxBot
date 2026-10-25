import { lazy, Suspense, type ComponentProps } from "react";
import { Skeleton } from "./skeleton";
const Content = lazy(() =>
  import("qrcode.react").then((module) => ({ default: module.QRCodeSVG })),
);

/** 官方授权码出现后才加载编码器；占位保持二维码真实尺寸，避免扫码区域跳动。 */
export function QRCodeSVG(props: ComponentProps<typeof Content>) {
  const size = props.size ?? 128;
  return (
    <Suspense
      fallback={
        <div
          className={props.className}
          style={{ width: size, height: size }}
          role="status"
          aria-busy="true"
          aria-label={props.title}
        >
          <Skeleton className="h-full w-full" />
        </div>
      }
    >
      <Content {...props} />
    </Suspense>
  );
}
