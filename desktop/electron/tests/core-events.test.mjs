import assert from "node:assert/strict";
import { test } from "node:test";
import { loadModule } from "./load-module.mjs";
const { CoreEventClient } = loadModule(
  "@neuxbot/platform-contracts/core-event-client",
);
const { globalEventSubscriptionSchema } = loadModule(
  "@neuxbot/platform-contracts/core-event-protocol",
);
const flush = () => new Promise((resolve) => setImmediate(resolve));
class Socket {
  readyState = 0;
  sent = [];
  closeListeners = new Set();
  addEventListener(type, listener) {
    if (type === "close") this.closeListeners.add(listener);
  }
  removeEventListener(type, listener) {
    if (type === "close") this.closeListeners.delete(listener);
  }
  close() {
    this.readyState = 3;
    for (const listener of this.closeListeners) listener();
  }
  send(data) {
    this.sent.push(JSON.parse(data));
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  frame(sequence, fields = {}) {
    this.onmessage?.({
      data: JSON.stringify({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: sequence,
        topic: "chat",
        type: "chat.event",
        resource: { kind: "chat", id: "chat-1" },
        eventId: `event-${sequence}`,
        payload: {},
        ...fields,
      }),
    });
  }
}
function setup(t) {
  const sockets = [];
  const targets = [];
  const client = new CoreEventClient({
    connection: async () => ({
      baseUrl: "http://127.0.0.1:8432",
      token: "secret",
    }),
    createSocket: (url, protocols) => {
      const socket = new Socket();
      sockets.push(socket);
      targets.push({ url, protocols });
      return socket;
    },
  });
  t.after(() => client.close());
  return { client, sockets, targets };
}
test("多资源共享一条物理连接，同一资源最后一个消费者释放才退订", async (t) => {
  const { client, sockets, targets } = setup(t);
  const first = [];
  const second = [];
  const stopFirst = client.subscribe(
    "chat",
    { kind: "chat", id: "chat-1" },
    { after: 4, onEvent: (frame) => first.push(frame) },
  );
  const stopSecond = client.subscribe(
    "chat",
    { kind: "chat", id: "chat-1" },
    { after: 4, onEvent: (frame) => second.push(frame) },
  );
  const stopSystem = client.subscribe(
    "system",
    { kind: "global" },
    { onEvent() {} },
  );
  await flush();
  assert.equal(sockets.length, 1);
  assert.deepEqual(targets[0], {
    url: "ws://127.0.0.1:8432/api/v1/events",
    protocols: ["ally.events.v1", "ally.auth.secret"],
  });
  const socket = sockets[0];
  socket.open();
  assert.equal(
    socket.sent.filter(
      (frame) => frame.topic === "chat" && frame.type === "subscribe",
    ).length,
    1,
  );
  socket.frame(1, { sourceCursor: 5 });
  assert.equal(first.length, 1);
  assert.equal(second.length, 1);
  stopFirst();
  assert.equal(
    socket.sent.filter((frame) => frame.type === "unsubscribe").length,
    0,
  );
  stopSecond();
  assert.equal(
    socket.sent.filter((frame) => frame.type === "unsubscribe").length,
    1,
  );
  assert.equal(socket.readyState, 1);
  stopSystem();
  assert.equal(socket.readyState, 3);
});
test("发现交付缺口和重连按权威来源恢复，transient 不推进恢复游标", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, sockets } = setup(t);
  const reasons = [];
  const received = [];
  client.subscribe(
    "chat",
    { kind: "chat", id: "chat-1" },
    {
      after: 4,
      onEvent: (frame) => received.push(frame),
      onResync: (reason) => reasons.push(reason),
    },
  );
  await flush();
  sockets[0].open();
  sockets[0].frame(1, { sourceCursor: 5 });
  sockets[0].frame(3, { transient: true, sourceCursor: 99 });
  assert.deepEqual(reasons, ["delivery_gap"]);
  sockets[0].frame(3, { sourceCursor: 5 });
  assert.equal(received.length, 2);
  sockets[0].onclose();
  t.mock.timers.tick(301);
  await flush();
  sockets[1].open();
  assert.equal(sockets[1].sent[0].after, 5);
  assert.ok(reasons.includes("reconnected"));
});
test("全局主题接收具体资源变化，控制回执不作为业务事件，心跳保持空闲连接", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, sockets } = setup(t);
  const received = [];
  client.subscribe(
    "entities",
    { kind: "global" },
    { onEvent: (frame) => received.push(frame) },
  );
  await flush();
  sockets[0].open();
  sockets[0].frame(1, {
    topic: "entities",
    type: "subscribed",
    resource: { kind: "global" },
    payload: { requestId: "1" },
  });
  sockets[0].frame(2, {
    topic: "entities",
    type: "entities.changed",
    resource: { kind: "waker", id: "waker-1" },
  });
  assert.equal(received.length, 1);
  t.mock.timers.tick(40_000);
  sockets[0].frame(3, {
    topic: "system",
    type: "heartbeat",
    resource: { kind: "global" },
  });
  t.mock.timers.tick(40_000);
  assert.equal(sockets[0].readyState, 1);
});
test("订阅只接受固定协议字段，拒绝任意URL、未知主题与超长身份", () => {
  const input = {
    subscriptionId: "00000000-0000-4000-8000-000000000001",
    topic: "chat",
    resource: { kind: "chat", id: "chat-1" },
  };
  assert.equal(globalEventSubscriptionSchema.safeParse(input).success, true);
  assert.equal(
    globalEventSubscriptionSchema.safeParse({
      ...input,
      url: "https://example.com",
    }).success,
    false,
  );
  assert.equal(
    globalEventSubscriptionSchema.safeParse({ ...input, topic: "private" })
      .success,
    false,
  );
  assert.equal(
    globalEventSubscriptionSchema.safeParse({
      ...input,
      resource: { kind: "chat", id: "x".repeat(201) },
    }).success,
    false,
  );
});

test("资源数据库暂时失败在同一连接内退避重订阅，成功后恢复，过期回执不干扰", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, sockets } = setup(t);
  const reasons = [];
  const states = [];
  const stop = client.subscribe(
    "chat",
    { kind: "chat", id: "chat-1" },
    {
      after: 4,
      onEvent() {},
      onResync: (reason) => reasons.push(reason),
      onState: (state) => states.push(state),
    },
  );
  await flush();
  const socket = sockets[0];
  socket.open();
  const first = socket.sent[0];
  socket.frame(1, {
    type: "error",
    payload: {
      requestId: first.requestId,
      code: "DatabaseBusy",
      retryable: true,
    },
  });
  assert.equal(states.at(-1), "unavailable");
  assert.equal(reasons.length, 0);
  t.mock.timers.tick(199);
  assert.equal(socket.sent.length, 1);
  t.mock.timers.tick(102);
  assert.equal(socket.sent.length, 2);
  assert.equal(sockets.length, 1);
  assert.equal(socket.sent[1].after, 4);
  socket.frame(2, {
    type: "error",
    payload: {
      requestId: first.requestId,
      code: "BadRequest",
      retryable: false,
    },
  });
  assert.equal(reasons.length, 0);
  socket.frame(3, {
    type: "subscribed",
    payload: { requestId: socket.sent[1].requestId },
  });
  assert.deepEqual(reasons, ["resource_recovered"]);
  assert.equal(states.at(-1), "open");
  stop();
  t.mock.timers.tick(20_000);
  assert.equal(socket.sent.length, 3);
});

test("缺失、取消或非法资源不自动重试；原生system无需恢复回调也能重订阅", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { installIMPowerLifecycle } = loadModule("main/im-power-lifecycle.ts");
  const { client, sockets } = setup(t);
  const started = [];
  const stop = installIMPowerLifecycle(
    {
      start: (kind) => {
        started.push(kind);
        return 1;
      },
      stop() {},
      isStarted: () => false,
    },
    client,
  );
  await flush();
  const socket = sockets[0];
  socket.open();
  socket.frame(1, {
    topic: "system",
    type: "error",
    resource: { kind: "global" },
    payload: {
      requestId: socket.sent[0].requestId,
      code: "DatabaseError",
      retryable: true,
    },
  });
  t.mock.timers.tick(301);
  assert.equal(socket.sent.length, 2);
  socket.frame(2, {
    topic: "system",
    type: "subscribed",
    resource: { kind: "global" },
    payload: { requestId: socket.sent[1].requestId },
  });
  socket.frame(3, {
    topic: "system",
    type: "system.updated",
    resource: { kind: "global" },
    payload: { keepAwake: true },
  });
  assert.deepEqual(started, ["prevent-app-suspension"]);
  socket.frame(4, {
    topic: "system",
    type: "error",
    resource: { kind: "global" },
    payload: {
      requestId: socket.sent[1].requestId,
      code: "OperationCanceled",
      retryable: false,
    },
  });
  t.mock.timers.tick(10_000);
  assert.equal(socket.sent.length, 2);
  stop();
});

test("重复资源失败退避有上限，退订取消资源恢复定时器", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { client, sockets } = setup(t);
  const stop = client.subscribe(
    "chat",
    { kind: "chat", id: "chat-1" },
    { onEvent() {} },
  );
  await flush();
  const socket = sockets[0];
  socket.open();
  for (let attempt = 0; attempt < 8; attempt++) {
    const request = socket.sent.at(-1);
    socket.frame(attempt + 1, {
      type: "error",
      payload: {
        requestId: request.requestId,
        code: "DatabaseBusy",
        retryable: true,
      },
    });
    const count = socket.sent.length;
    t.mock.timers.tick(Math.min(301 * 2 ** attempt, 10_000));
    assert.equal(socket.sent.length, count + 1);
  }
  const last = socket.sent.at(-1);
  socket.frame(9, {
    type: "error",
    payload: {
      requestId: last.requestId,
      code: "DatabaseBusy",
      retryable: true,
    },
  });
  stop();
  const count = socket.sent.length;
  t.mock.timers.tick(10_000);
  assert.equal(socket.sent.length, count);
});
