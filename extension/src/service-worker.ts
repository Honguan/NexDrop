import { connectPresence, DirectError, sendDirect } from "./direct.js";
import { SharePayload, openWebShare } from "./protocol.js";

const menuItems: Array<chrome.contextMenus.CreateProperties> = [
  { id: "nexdrop-page", title: "傳送目前頁面到 NexDrop", contexts: ["page"] },
  { id: "nexdrop-selection", title: "傳送選取文字到 NexDrop", contexts: ["selection"] },
  { id: "nexdrop-link", title: "傳送連結到 NexDrop", contexts: ["link"] },
  { id: "nexdrop-image", title: "傳送圖片網址到 NexDrop", contexts: ["image"] },
];

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() =>
    menuItems.forEach((item) => chrome.contextMenus.create(item)),
  );
  void maintainPresence();
});

let presence: WebSocket | null = null;
let heartbeat: ReturnType<typeof setInterval> | undefined;
let reconnect: ReturnType<typeof setTimeout> | undefined;
let presenceEnabled = true;

async function maintainPresence() {
  if (!presenceEnabled) return;
  if (presence && presence.readyState <= WebSocket.OPEN) return;
  try {
    presence = await connectPresence();
    presence.addEventListener("open", () => {
      heartbeat = setInterval(() => presence?.send(JSON.stringify({ type: "heartbeat" })), 15000);
    });
    presence.addEventListener("close", schedulePresenceReconnect);
    presence.addEventListener("error", schedulePresenceReconnect);
  } catch {
    schedulePresenceReconnect();
  }
}

function schedulePresenceReconnect() {
  if (heartbeat) clearInterval(heartbeat);
  if (reconnect) clearTimeout(reconnect);
  presence = null;
  if (!presenceEnabled) return;
  reconnect = setTimeout(() => void maintainPresence(), 30000);
}

void maintainPresence();

chrome.contextMenus.onClicked.addListener((info, tab) => {
  const payload = payloadFromMenu(info, tab);
  if (payload) void openWebShare(payload);
});

chrome.runtime.onMessage.addListener(
  (
    message: { type?: string; payload?: SharePayload },
    _sender,
    respond,
  ) => {
    if (message.type === "presence_reconnect") {
      presenceEnabled = true;
      void maintainPresence();
      respond({ ok: true });
      return false;
    }
    if (message.type === "presence_disconnect") {
      presenceEnabled = false;
      if (heartbeat) clearInterval(heartbeat);
      if (reconnect) clearTimeout(reconnect);
      presence?.close();
      presence = null;
      respond({ ok: true });
      return false;
    }
    if (message.type !== "share" || !message.payload) return false;
    sendDirect(message.payload)
      .then(() => respond({ ok: true }))
      .catch((error: unknown) =>
        respond({
          ok: false,
          error: error instanceof Error ? error.message : "SEND_FAILED",
          retryAfterSeconds:
            error instanceof DirectError
              ? error.retryAfterSeconds
              : undefined,
        }),
      );
    return true;
  },
);

function payloadFromMenu(info: chrome.contextMenus.OnClickData, tab?: chrome.tabs.Tab): SharePayload | null {
  if (info.menuItemId === "nexdrop-selection") {
    return {
      kind: "SELECTION",
      text: info.selectionText,
      title: tab?.title,
      url: tab?.url,
    };
  }
  if (info.menuItemId === "nexdrop-link") {
    return { kind: "LINK", url: info.linkUrl, title: tab?.title };
  }
  if (info.menuItemId === "nexdrop-image") {
    return { kind: "IMAGE", url: info.srcUrl, title: tab?.title };
  }
  if (info.menuItemId === "nexdrop-page") {
    return { kind: "PAGE", url: tab?.url, title: tab?.title };
  }
  return null;
}
