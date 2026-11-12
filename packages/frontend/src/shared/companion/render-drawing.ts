import type { SvgDrawing } from "@neuxbot/bloub/drawing";
import { createElement } from "react";

export function renderDrawing(
  drawing: SvgDrawing,
  key: number,
): ReturnType<typeof createElement> {
  return createElement(
    drawing.tag,
    { ...drawing.attributes, key },
    ...drawing.children.map(renderDrawing),
  );
}
