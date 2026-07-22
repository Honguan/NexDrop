import assert from "node:assert/strict";
import test from "node:test";

import { subscribeNodeEvents } from "../src/realtime.ts";

test("realtime subscription owns heartbeat, acknowledgement, and reconnect", () => {
  const sockets = [];
  const intervals = new Map();
  const timeouts = new Map();
  let nextTimer = 1;
  const environment = {
    createSocket(url) {
      const socket = {
        url,
        onopen: null,
        onmessage: null,
        onclose: null,
        sent: [],
        closed: false,
        send(value) {
          this.sent.push(JSON.parse(value));
        },
        close() {
          this.closed = true;
        },
      };
      sockets.push(socket);
      return socket;
    },
    setInterval(callback, milliseconds) {
      const id = nextTimer++;
      intervals.set(id, { callback, milliseconds });
      return id;
    },
    clearInterval(id) {
      intervals.delete(id);
    },
    setTimeout(callback, milliseconds) {
      const id = nextTimer++;
      timeouts.set(id, { callback, milliseconds });
      return id;
    },
    clearTimeout(id) {
      timeouts.delete(id);
    },
  };
  const online = [];
  const notifications = [];
  let refreshes = 0;
  let token = "token-1";

  const stop = subscribeNodeEvents(
    () => `wss://node.example/ws?access_token=${token}`,
    {
      onOnlineChange: (value) => online.push(value),
      onRefresh: () => refreshes++,
      onNotification: (value) => notifications.push(value.id),
    },
    environment,
  );

  assert.equal(sockets.length, 1);
  assert.equal(sockets[0].url, "wss://node.example/ws?access_token=token-1");
  sockets[0].onopen();
  sockets[0].onmessage({ data: JSON.stringify({ type: "connected" }) });
  assert.deepEqual(online, [true]);
  assert.equal(refreshes, 1);
  assert.equal([...intervals.values()][0].milliseconds, 5000);

  [...intervals.values()][0].callback();
  assert.deepEqual(sockets[0].sent[0], { type: "heartbeat" });
  sockets[0].onmessage({
    data: JSON.stringify({ type: "notification", notification: { id: "n-1" } }),
  });
  assert.deepEqual(notifications, ["n-1"]);
  assert.deepEqual(sockets[0].sent[1], {
    type: "notification_ack",
    notificationId: "n-1",
  });
  assert.equal(refreshes, 2);

  assert.doesNotThrow(() => sockets[0].onmessage({ data: "not-json" }));
  sockets[0].onclose();
  assert.deepEqual(online, [true, false]);
  assert.equal([...timeouts.values()][0].milliseconds, 3000);
  token = "token-2";
  [...timeouts.values()][0].callback();
  assert.equal(sockets.length, 2);
  assert.equal(sockets[1].url, "wss://node.example/ws?access_token=token-2");

  stop();
  assert.equal(sockets[1].closed, true);
  assert.deepEqual(online, [true, false, false]);
  assert.equal(intervals.size, 0);
  assert.equal(timeouts.size, 0);
});
