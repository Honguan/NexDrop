import { SharePayload, openWebShare, requestID, requestNative } from "./protocol.js";

const menuItems: Array<chrome.contextMenus.CreateProperties> = [
  { id: "nexdrop-page", title: "傳送目前頁面到 NexDrop", contexts: ["page"] },
  { id: "nexdrop-selection", title: "傳送選取文字到 NexDrop", contexts: ["selection"] },
  { id: "nexdrop-link", title: "傳送連結到 NexDrop", contexts: ["link"] },
  { id: "nexdrop-image", title: "傳送圖片網址到 NexDrop", contexts: ["image"] },
];

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => menuItems.forEach((item) => chrome.contextMenus.create(item)));
});

chrome.contextMenus.onClicked.addListener((info, tab) => {
  const payload = payloadFromMenu(info, tab);
  if (!payload) return;
  requestNative({ id: requestID(), type: "share", payload })
    .then((response) => {
      if (!response.ok) return openWebShare(payload);
    })
    .catch(() => openWebShare(payload));
});

chrome.runtime.onMessage.addListener((message: { type?: string; payload?: SharePayload }, _sender, respond) => {
  if (message.type !== "share" || !message.payload) return false;
  requestNative({ id: requestID(), type: "share", payload: message.payload })
    .then((response) => respond(response))
    .catch(async () => {
      await openWebShare(message.payload!);
      respond({ ok: true, fallback: true });
    });
  return true;
});

function payloadFromMenu(info: chrome.contextMenus.OnClickData, tab?: chrome.tabs.Tab): SharePayload | null {
  if (info.menuItemId === "nexdrop-selection") return { kind: "SELECTION", text: info.selectionText, title: tab?.title, url: tab?.url };
  if (info.menuItemId === "nexdrop-link") return { kind: "LINK", url: info.linkUrl, title: tab?.title };
  if (info.menuItemId === "nexdrop-image") return { kind: "IMAGE", url: info.srcUrl, title: tab?.title };
  if (info.menuItemId === "nexdrop-page") return { kind: "PAGE", url: tab?.url, title: tab?.title };
  return null;
}
