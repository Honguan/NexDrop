type RealtimeSocket = Pick<
  WebSocket,
  "onopen" | "onmessage" | "onclose" | "send" | "close"
>;

export type RealtimeEnvironment = {
  createSocket(url: string): RealtimeSocket;
  setInterval(callback: () => void, milliseconds: number): number;
  clearInterval(id: number): void;
  setTimeout(callback: () => void, milliseconds: number): number;
  clearTimeout(id: number): void;
};

export type RealtimeHandlers = {
  onOnlineChange(online: boolean): void;
  onRefresh(): void;
  onNotification(notification: { id: string }): void;
};

type NodeEvent = {
  type: string;
  notification?: { id: string };
};

export function subscribeNodeEvents(
  url: string,
  handlers: RealtimeHandlers,
  environment: RealtimeEnvironment = browserEnvironment(),
) {
  let socket: RealtimeSocket | undefined;
  let heartbeat: number | undefined;
  let reconnect: number | undefined;
  let stopped = false;

  const clearHeartbeat = () => {
    if (heartbeat === undefined) return;
    environment.clearInterval(heartbeat);
    heartbeat = undefined;
  };

  const connect = () => {
    if (stopped) return;
    socket = environment.createSocket(url);
    socket.onopen = () => handlers.onOnlineChange(true);
    socket.onmessage = (event) => {
      const message = parseNodeEvent(event.data);
      if (!message) return;
      if (message.type === "connected") {
        handlers.onRefresh();
        clearHeartbeat();
        heartbeat = environment.setInterval(
          () => socket?.send(JSON.stringify({ type: "heartbeat" })),
          5000,
        );
      }
      if (message.type === "heartbeat_ack") handlers.onRefresh();
      if (message.type === "notification" && message.notification) {
        socket?.send(
          JSON.stringify({
            type: "notification_ack",
            notificationId: message.notification.id,
          }),
        );
        handlers.onNotification(message.notification);
        handlers.onRefresh();
      }
    };
    socket.onclose = () => {
      handlers.onOnlineChange(false);
      clearHeartbeat();
      if (!stopped) reconnect = environment.setTimeout(connect, 3000);
    };
  };

  connect();
  return () => {
    stopped = true;
    clearHeartbeat();
    if (reconnect !== undefined) environment.clearTimeout(reconnect);
    if (socket) {
      socket.onclose = null;
      socket.close();
    }
    handlers.onOnlineChange(false);
  };
}

function parseNodeEvent(value: unknown): NodeEvent | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const parsed = JSON.parse(value) as Partial<NodeEvent>;
    if (!parsed || typeof parsed.type !== "string") return undefined;
    if (
      parsed.notification !== undefined &&
      (!parsed.notification || typeof parsed.notification.id !== "string")
    ) {
      return undefined;
    }
    return parsed as NodeEvent;
  } catch {
    return undefined;
  }
}

function browserEnvironment(): RealtimeEnvironment {
  return {
    createSocket: (url) => new WebSocket(url, "nexdrop.v1"),
    setInterval: (callback, milliseconds) =>
      window.setInterval(callback, milliseconds),
    clearInterval: (id) => window.clearInterval(id),
    setTimeout: (callback, milliseconds) =>
      window.setTimeout(callback, milliseconds),
    clearTimeout: (id) => window.clearTimeout(id),
  };
}
