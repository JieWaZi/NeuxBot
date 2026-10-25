/** 为原生嵌套 repeat 保存完整结果内对象的 JSON Pointer，避免复制或截断数组。 */
export function resultDataPaths(result: unknown): WeakMap<object, string> {
  const paths = new WeakMap<object, string>();
  const visit = (value: unknown, path: string, depth: number) => {
    if (!value || typeof value !== "object" || depth > 64 || paths.has(value))
      return;
    paths.set(value, path);
    for (const [key, child] of Object.entries(value))
      visit(
        child,
        `${path}/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`,
        depth + 1,
      );
  };
  visit(result, "/result", 0);
  return paths;
}

import {
  getByPath,
  resolveElementProps,
  resolveRepeatStatePath,
  type PropResolutionContext,
  type Spec,
} from "@json-render/core";

/** 使用官方表达式解析器预检展开上限；分页集合按每页最大值计数，不截断真实数据。 */
export function withinResultBudget(spec: Spec, result: unknown): boolean {
  const dataPaths = resultDataPaths(result);
  let count = 0;
  const stateModel = { result };
  const visit = (
    id: string,
    context: PropResolutionContext,
    depth: number,
  ): boolean => {
    if (++count > 2000 || depth > 32) return false;
    const element = spec.elements[id];
    if (!element) return false;
    const children = element.children ?? [];
    if (element.repeat) {
      const path = resolveRepeatStatePath(
        element.repeat.statePath,
        context.repeatBasePath,
      );
      const items = path ? getByPath(stateModel, path) : undefined;
      if (!Array.isArray(items)) return true;
      if (items.length > 50) return false;
      return items.every((item, index) =>
        children.every((child) =>
          visit(
            child,
            {
              stateModel,
              repeatItem: item,
              repeatIndex: index,
              repeatBasePath: `${path}/${index}`,
            },
            depth + 1,
          ),
        ),
      );
    }
    if (element.type === "Collection") {
      const props = resolveElementProps(element.props, context);
      const items = Array.isArray(props.items) ? props.items : [];
      const size = typeof props.pageSize === "number" ? props.pageSize : 10;
      // 检查所有项的结构上限，计数取最重的一页，避免后续翻页才突破预算。
      const start = count;
      let worst = 0;
      for (let offset = 0; offset < items.length; offset += size) {
        count = start;
        for (
          let index = offset;
          index < Math.min(offset + size, items.length);
          index++
        ) {
          for (const child of children)
            if (
              !visit(
                child,
                {
                  stateModel,
                  repeatItem: items[index],
                  repeatIndex: index,
                  repeatBasePath: `${dataPaths.get(items) ?? "/result"}/${index}`,
                },
                depth + 1,
              )
            )
              return false;
        }
        worst = Math.max(worst, count - start);
      }
      count = start + worst;
      return true;
    }
    return children.every((child) => visit(child, context, depth + 1));
  };
  try {
    return visit(spec.root, { stateModel }, 0);
  } catch {
    return false;
  }
}
