import { Bodies, Body, Composite, Engine, Sleeping } from "matter-js";

const step = 1000 / 60;
const maxDuration = 3600;
const dropInterval = 52;
const settleDelay = 360;
const returnDuration = 480;

// 使用轮廓内的碰撞半径，头像画布的透明留白不参与碰撞。
const radiusRatio = 0.36;

export function dropWorkBoardAvatars(stage: HTMLElement, enter = true) {
  const width = stage.clientWidth;
  const height = stage.clientHeight;
  if (!width || !height) return;
  const engine = Engine.create({ enableSleeping: true, positionIterations: 8 });
  const floor = height - 12;
  const bounds = enter ? undefined : stage.getBoundingClientRect();
  const wall = 24;
  const wallHeight = (height + 160) * 2;
  Composite.add(engine.world, [
    Bodies.rectangle(width / 2, floor + 20, width + 80, 40, { isStatic: true }),
    Bodies.rectangle(-wall / 2, 0, wall, wallHeight, { isStatic: true }),
    Bodies.rectangle(width + wall / 2, 0, wall, wallHeight, { isStatic: true }),
  ]);

  const pieces = [
    ...stage.querySelectorAll<HTMLElement>("[data-pile-avatar]"),
  ].map((node, index) => {
    const diameter = node.offsetWidth;
    const radius = Math.max(1, diameter * radiusRatio);
    const fraction =
      Number.parseFloat(node.style.getPropertyValue("--pile-rest-x")) / 100;
    let x = width * fraction;
    let y = -diameter / 2;
    if (bounds) {
      const position = node.getBoundingClientRect();
      x = position.left + position.width / 2 - bounds.left;
      y = position.top + position.height / 2 - bounds.top;
    }
    const angle = enter
      ? (index % 2 ? 1 : -1) * 0.16
      : node.hasAttribute("data-falling")
        ? Number.parseFloat(node.style.getPropertyValue("--pile-live-angle"))
        : (Number.parseFloat(node.style.getPropertyValue("--pile-rest-angle")) *
            Math.PI) /
          180;
    const body = Bodies.circle(
      Math.max(radius, Math.min(width - radius, x)),
      y,
      radius,
      {
        angle,
        restitution: 0.26,
        friction: 0.55,
        frictionAir: 0.022,
        sleepThreshold: 26,
      },
    );
    return {
      node,
      body,
      diameter,
      rest: { x: 0, y: 0, angle: 0 },
      returnFrom: { x: 0, y: 0, angle: 0 },
    };
  });
  let frame = 0;
  let time = 0;
  let last = 0;
  let accumulator = 0;
  let released = 0;
  let entered = !enter;
  let lastNudge = 0;
  let returnStarted = 0;
  let pointer: { x: number; y: number } | undefined;

  function draw(atRest = false) {
    for (const { node, body, diameter, rest } of pieces) {
      const { x, y } = atRest ? rest : body.position;
      const angle = atRest ? rest.angle : body.angle;
      node.style.setProperty("--pile-live-x", `${(x / width) * 100}%`);
      node.style.setProperty(
        "--pile-live-y",
        `calc(12px + var(--pile-size) * ${(floor - y) / diameter})`,
      );
      node.style.setProperty("--pile-live-angle", `${angle}rad`);
      node.dataset.falling = "";
    }
  }

  function queueFrame() {
    if (!frame) frame = requestAnimationFrame(tick);
  }

  function brush(event: PointerEvent) {
    if (!entered || event.pointerType === "touch") return;
    const bounds = stage.getBoundingClientRect();
    const next = {
      x: (event.clientX - bounds.left) * (width / (bounds.width || width)),
      y: (event.clientY - bounds.top) * (height / (bounds.height || height)),
    };
    const previous = pointer;
    pointer = next;
    if (!previous) return;
    const dx = next.x - previous.x;
    const dy = next.y - previous.y;
    const speed = Math.hypot(dx, dy);
    if (speed < 1) return;

    let nudged = false;
    for (const { body, diameter } of pieces) {
      const { x, y } = body.position;
      // 快速划过时用光标路径判断接触，避免从小头像的一侧跳到另一侧。
      const fraction = Math.max(
        0,
        Math.min(
          1,
          ((x - previous.x) * dx + (y - previous.y) * dy) / (speed * speed),
        ),
      );
      const distance = Math.hypot(
        x - previous.x - fraction * dx,
        y - previous.y - fraction * dy,
      );
      const reach = diameter * radiusRatio + 15;
      if (distance >= reach) continue;
      const force =
        (1 - distance / reach) * Math.min(speed, 22) * Math.sqrt(75 / diameter);
      Sleeping.set(body, false);
      Body.setVelocity(body, {
        x: Math.max(
          -7,
          Math.min(7, body.velocity.x * 0.6 + (dx / speed) * force * 0.48),
        ),
        y: Math.max(
          -5.5,
          Math.min(
            2,
            body.velocity.y * 0.4 + (dy / speed - 0.7) * force * 0.35,
          ),
        ),
      });
      Body.setAngularVelocity(body, (dx / speed) * force * 0.009);
      nudged = true;
    }
    if (nudged) {
      returnStarted = 0;
      lastNudge = time;
      queueFrame();
    }
  }

  function clearPointer() {
    pointer = undefined;
  }

  function tick(now: number) {
    frame = 0;
    accumulator += last ? Math.min(now - last, 50) : step;
    last = now;
    while (accumulator >= step) {
      const next = pieces[released];
      if (next && time >= released * dropInterval) {
        Composite.add(engine.world, next.body);
        Body.setAngularVelocity(next.body, (released % 2 ? -1 : 1) * 0.023);
        released++;
      }
      if (!returnStarted) Engine.update(engine, step);
      time += step;
      accumulator -= step;
    }
    if (
      !entered &&
      (time >= maxDuration ||
        (released === pieces.length &&
          pieces.every(({ body }) => body.isSleeping)))
    ) {
      entered = true;
      for (const piece of pieces) {
        piece.rest = { ...piece.body.position, angle: piece.body.angle };
        Sleeping.set(piece.body, true);
      }
      draw();
      last = 0;
      return;
    }

    if (entered && time - lastNudge >= settleDelay) {
      if (!returnStarted) {
        returnStarted = time;
        for (const piece of pieces) {
          piece.returnFrom = {
            ...piece.body.position,
            angle: piece.body.angle,
          };
          Sleeping.set(piece.body, true);
        }
      }
      const progress = Math.min(1, (time - returnStarted) / returnDuration);
      const ease = 1 - (1 - progress) ** 3;
      for (const { body, rest, returnFrom } of pieces) {
        Body.setPosition(body, {
          x: returnFrom.x + (rest.x - returnFrom.x) * ease,
          y: returnFrom.y + (rest.y - returnFrom.y) * ease,
        });
        Body.setAngle(
          body,
          returnFrom.angle + (rest.angle - returnFrom.angle) * ease,
        );
      }
      if (progress === 1) {
        draw();
        last = 0;
        return;
      }
    }
    draw();
    queueFrame();
  }

  stage.addEventListener("pointermove", brush, { passive: true });
  stage.addEventListener("pointerleave", clearPointer);
  if (enter) {
    queueFrame();
  } else {
    released = pieces.length;
    Composite.add(
      engine.world,
      pieces.map(({ body }) => body),
    );
    for (const piece of pieces) {
      piece.rest = { ...piece.body.position, angle: piece.body.angle };
      Sleeping.set(piece.body, true);
    }
  }
  return () => {
    stage.removeEventListener("pointermove", brush);
    stage.removeEventListener("pointerleave", clearPointer);
    cancelAnimationFrame(frame);
    Composite.clear(engine.world, false);
    Engine.clear(engine);
    if (entered) {
      draw(true);
    } else {
      for (const { node } of pieces) delete node.dataset.falling;
    }
  };
}
