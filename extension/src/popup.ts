import { directStatus } from "./direct.js";
import { SharePayload, nodeURL } from "./protocol.js";

const statusElement = document.querySelector<HTMLElement>("#status")!;
const targetsElement = document.querySelector<HTMLElement>("#targets")!;
const contentInput = document.querySelector<HTMLTextAreaElement>("#content")!;
const includeURL = document.querySelector<HTMLInputElement>("#include-url")!;
const sendButton = document.querySelector<HTMLButtonElement>("#send")!;
const openButton = document.querySelector<HTMLButtonElement>("#open-web")!;

void initialize();

async function initialize() {
  const preference = await chrome.storage.sync.get({ includeCurrentURL: true });
  includeURL.checked = Boolean(preference.includeCurrentURL);
  includeURL.addEventListener("change", () => chrome.storage.sync.set({ includeCurrentURL: includeURL.checked }));
  openButton.addEventListener("click", async () => chrome.tabs.create({ url: await nodeURL() }));
  sendButton.addEventListener("click", sendContent);
  try {
    const status = await directStatus();
    if (!status) throw new Error("NOT_PAIRED");
    statusElement.textContent = status.pending ? "等待核准" : status.connected ? "擴充功能已配對" : "節點離線";
    statusElement.classList.toggle("offline", !status.connected);
    renderTargets(status.devices.map((device) => ({ id: device.id, name: device.displayName, online: Boolean(device.online) })));
    sendButton.disabled = !status.connected;
  } catch {
    statusElement.textContent = "擴充功能未配對";
    statusElement.classList.add("offline");
    targetsElement.innerHTML = '<p class="empty">請先開啟「配對設定」登入節點並核准此擴充功能。</p>';
  }
}

function renderTargets(devices: Array<{ id: string; name: string; online: boolean }>) {
  const nodes = devices.map((device) => {
    const label = document.createElement("label");
    label.className = "target";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.name = "target";
    input.value = device.id;
    input.checked = true;
    const text = document.createElement("span");
    const name = document.createElement("strong");
    name.textContent = device.name;
    const detail = document.createElement("small");
    detail.textContent = device.online ? "在線" : "離線，可排隊傳送";
    text.append(name, detail);
    label.append(input, text);
    return label;
  });
  targetsElement.replaceChildren(...nodes);
  if (!nodes.length) targetsElement.innerHTML = '<p class="empty">尚無其他已核准設備</p>';
}

async function sendContent() {
  const targets = Array.from(document.querySelectorAll<HTMLInputElement>('#targets input[name="target"]:checked')).map((input) => input.value);
  if (!targets.length) {
    showResult("請選擇接收設備", false);
    return;
  }
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const parts = [contentInput.value.trim()];
  if (includeURL.checked && tab?.url) parts.push(tab.url);
  const text = parts.filter(Boolean).join("\n\n");
  if (!text) {
    showResult("請輸入內容或勾選目前網址", false);
    return;
  }
  sendButton.disabled = true;
  const payload: SharePayload = { kind: includeURL.checked ? "PAGE" : "SELECTION", title: tab?.title, url: includeURL.checked ? tab?.url : undefined, text, targetDeviceIds: targets };
  const response = await chrome.runtime.sendMessage({ type: "share", payload });
  showResult(response?.ok ? "已安全送出" : messageFor(response?.error, response?.retryAfterSeconds), Boolean(response?.ok));
  if (response?.ok) window.setTimeout(() => window.close(), 800);
  else sendButton.disabled = false;
}

function showResult(message: string, success: boolean) {
  const result = document.querySelector<HTMLElement>("#send-result")!;
  result.textContent = message;
  result.className = success ? "success" : "error";
}

function messageFor(code?: string, retryAfterSeconds?: number) {
  if (code === "RATE_LIMITED") return retryAfterSeconds ? `操作太頻繁，請在 ${retryAfterSeconds} 秒後再試。` : "操作太頻繁，請稍後再試。";
  const messages: Record<string, string> = { TARGET_REQUIRED: "請選擇接收設備。", CONTENT_REQUIRED: "請輸入要傳送的內容。", TARGET_UNAVAILABLE: "接收設備目前不可用。", INVALID_TOKEN: "登入已過期，請重新配對。" };
  return messages[code ?? ""] ?? "傳送失敗，請檢查節點連線。";
}
