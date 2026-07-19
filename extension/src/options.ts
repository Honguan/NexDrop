import { DirectError, directStatus, disconnectExtension, pairExtension } from "./direct.js";
import { nodeURL } from "./protocol.js";

const form = document.querySelector<HTMLFormElement>("form")!;
const status = document.querySelector<HTMLElement>("#save-status")!;
const disconnect = document.querySelector<HTMLButtonElement>("#disconnect")!;
const importButton = document.querySelector<HTMLButtonElement>("#import-config")!;
const copyButton = document.querySelector<HTMLButtonElement>("#copy-config")!;

void initialize();

async function initialize() {
  document.querySelector<HTMLInputElement>("#node-url")!.value = await nodeURL();
  const stored = await chrome.storage.local.get("directNodeKey");
  document.querySelector<HTMLInputElement>("#node-key")!.value = String(stored.directNodeKey ?? "");
  try {
    const connected = await directStatus();
    if (connected) {
      status.textContent = "已使用節點密鑰加入。";
      status.className = "success";
      disconnect.hidden = false;
    }
  } catch {
    status.textContent = "既有節點工作階段已失效，請重新匯入節點設定。";
    status.className = "error";
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
  button.disabled = true;
  status.textContent = "正在加入節點…";
  try {
    await pairExtension(
      document.querySelector<HTMLInputElement>("#node-url")!.value,
      document.querySelector<HTMLInputElement>("#node-key")!.value,
      document.querySelector<HTMLInputElement>("#device-name")!.value,
    );
    status.textContent = "加入完成；此設備已直接連接節點。";
    status.className = "success";
    disconnect.hidden = false;
    await chrome.runtime.sendMessage({ type: "presence_reconnect" });
  } catch (error) {
    status.textContent = messageFor(error instanceof Error ? error.message : "ENROLL_FAILED", error instanceof DirectError ? error.retryAfterSeconds : undefined);
    status.className = "error";
  } finally {
    button.disabled = false;
  }
});

importButton.addEventListener("click", async () => {
  try {
    const text = await navigator.clipboard.readText();
    const value = JSON.parse(text) as { nodeUrl?: string; nodeURL?: string; nodeKey?: string };
    document.querySelector<HTMLInputElement>("#node-url")!.value = value.nodeUrl ?? value.nodeURL ?? "";
    document.querySelector<HTMLInputElement>("#node-key")!.value = value.nodeKey ?? "";
    status.textContent = "已從剪貼簿匯入；按「加入節點」完成綁定。";
    status.className = "success";
  } catch {
    status.textContent = "剪貼簿不是有效的 NexDrop 節點設定。";
    status.className = "error";
  }
});

copyButton.addEventListener("click", async () => {
  const config = JSON.stringify({
    nodeUrl: document.querySelector<HTMLInputElement>("#node-url")!.value.trim(),
    nodeKey: document.querySelector<HTMLInputElement>("#node-key")!.value.trim(),
  });
  await navigator.clipboard.writeText(config);
  status.textContent = "節點設定已複製。";
  status.className = "success";
});

disconnect.addEventListener("click", async () => {
  await chrome.runtime.sendMessage({ type: "presence_disconnect" });
  await disconnectExtension();
  status.textContent = "已中斷本機連線；離線設備可由管理後台刪除。";
  status.className = "success";
  disconnect.hidden = true;
});

function messageFor(code: string, retryAfterSeconds?: number) {
  if (code === "RATE_LIMITED") return retryAfterSeconds ? `操作太頻繁，請在 ${retryAfterSeconds} 秒後再試。` : "操作太頻繁，請稍後再試。";
  const messages: Record<string, string> = { INVALID_NODE_KEY: "節點密鑰不正確。", PERMISSION_DENIED: "必須允許此節點的網站權限。", INVALID_NODE_URL: "請輸入有效的 IP 或 HTTPS 節點網址。" };
  return messages[code] ?? `加入失敗：${code}`;
}
