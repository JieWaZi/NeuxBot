export type CanvasBounds = {
  left: number;
  top: number;
  width: number;
  height: number;
};
type Point = { x: number; y: number };
type Step = { id: number; cost: number; score: number };

/** 在阶段边缘构造稀疏正交网格，以曼哈顿距离和转弯代价执行 A* 避障寻路。 */
export function workflowConnectionRoute(
  source: CanvasBounds,
  target: CanvasBounds,
  bounds: CanvasBounds[],
): Point[] {
  const sameRow = source.top === target.top;
  const forward = sameRow ? source.left < target.left : source.top < target.top;
  const port = (box: CanvasBounds, outgoing: boolean, gap: number): Point => {
    const positive = outgoing === forward;
    return sameRow
      ? { x: box.left + (positive ? box.width + gap : -gap), y: box.top + 32 }
      : {
          x: box.left + box.width / 2,
          y: box.top + (positive ? box.height + gap : -gap),
        };
  };
  const start = port(source, true, 16);
  const end = port(target, false, 16);
  const obstacles = bounds.map((box) => ({
    left: box.left - 16,
    right: box.left + box.width + 16,
    top: box.top - 16,
    bottom: box.top + box.height + 16,
  }));
  const xs = [
    ...new Set([
      start.x,
      end.x,
      ...obstacles.flatMap((box) => [box.left, box.right]),
    ]),
  ].sort((a, b) => a - b);
  const ys = [
    ...new Set([
      start.y,
      end.y,
      ...obstacles.flatMap((box) => [box.top, box.bottom]),
    ]),
  ].sort((a, b) => a - b);
  const point = (id: number): Point => ({
    x: xs[Math.floor(id / 2) % xs.length] ?? 0,
    y: ys[Math.floor(id / 2 / xs.length)] ?? 0,
  });
  const clear = (a: Point, b: Point) =>
    !obstacles.some((box) =>
      a.x === b.x
        ? a.x > box.left &&
          a.x < box.right &&
          Math.max(a.y, b.y) > box.top &&
          Math.min(a.y, b.y) < box.bottom
        : a.y > box.top &&
          a.y < box.bottom &&
          Math.max(a.x, b.x) > box.left &&
          Math.min(a.x, b.x) < box.right,
    );
  const distance = (a: Point, b: Point) =>
    Math.abs(a.x - b.x) + Math.abs(a.y - b.y);
  const initial =
    (ys.indexOf(start.y) * xs.length + xs.indexOf(start.x)) * 2 +
    (sameRow ? 0 : 1);
  const goal = ys.indexOf(end.y) * xs.length + xs.indexOf(end.x);
  const costs = new Map([[initial, 0]]);
  const parents = new Map<number, number>();
  const queue: Step[] = [];
  const push = (step: Step) => {
    let index = queue.length;
    queue.push(step);
    while (index > 0) {
      const parent = Math.floor((index - 1) / 2);
      const previous = queue[parent];
      if (!previous || previous.score <= step.score) break;
      queue[index] = previous;
      index = parent;
    }
    queue[index] = step;
  };
  const pop = () => {
    const first = queue[0];
    const last = queue.pop();
    if (queue.length && last) {
      let index = 0;
      while (index * 2 + 1 < queue.length) {
        let child = index * 2 + 1;
        if (
          (queue[child + 1]?.score ?? Infinity) <
          (queue[child]?.score ?? Infinity)
        )
          child++;
        const next = queue[child];
        if (!next || next.score >= last.score) break;
        queue[index] = next;
        index = child;
      }
      queue[index] = last;
    }
    return first;
  };
  push({ id: initial, cost: 0, score: distance(start, end) });
  while (queue.length) {
    const current = pop();
    if (!current || current.cost !== costs.get(current.id)) continue;
    const cell = Math.floor(current.id / 2);
    if (cell === goal) {
      const route = [port(target, false, 4)];
      let id: number | undefined = current.id;
      while (id !== undefined) {
        route.push(point(id));
        id = parents.get(id);
      }
      route.push(port(source, true, 4));
      const compact: Point[] = [];
      for (const next of route.reverse()) {
        const last = compact.at(-1);
        if (last?.x === next.x && last.y === next.y) continue;
        const before = compact.at(-2);
        if (
          before &&
          last &&
          ((before.x === last.x && last.x === next.x) ||
            (before.y === last.y && last.y === next.y))
        )
          compact.pop();
        compact.push(next);
      }
      return compact;
    }
    const x = cell % xs.length;
    const y = Math.floor(cell / xs.length);
    const neighbors = [
      { x: x - 1, y, axis: 0 },
      { x: x + 1, y, axis: 0 },
      { x, y: y - 1, axis: 1 },
      { x, y: y + 1, axis: 1 },
    ];
    for (const next of neighbors) {
      if (
        next.x < 0 ||
        next.x >= xs.length ||
        next.y < 0 ||
        next.y >= ys.length
      )
        continue;
      const id = (next.y * xs.length + next.x) * 2 + next.axis;
      const position = point(id);
      if (!clear(point(current.id), position)) continue;
      const cost =
        current.cost +
        distance(point(current.id), position) +
        (current.id % 2 === next.axis ? 0 : 24);
      if (cost >= (costs.get(id) ?? Infinity)) continue;
      costs.set(id, cost);
      parents.set(id, current.id);
      push({ id, cost, score: cost + distance(position, end) });
    }
  }
  return [];
}

export function workflowConnectionPath(
  source: CanvasBounds,
  target: CanvasBounds,
  bounds: CanvasBounds[],
) {
  const points = workflowConnectionRoute(source, target, bounds);
  return points
    .map((point, index) => {
      const previous = points[index - 1];
      if (!previous) return `M${point.x} ${point.y}`;
      return point.x === previous.x ? `V${point.y}` : `H${point.x}`;
    })
    .join(" ");
}
