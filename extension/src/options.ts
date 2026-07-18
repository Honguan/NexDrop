import { DirectError, directStatus, disconnectExtension, pairExtension } from "./direct.js";
import { nodeURL } from "./protocol.js";

const form = document.querySelector<HTMLFormElement>("form")!;
const status = document.querySelector<HTMLElement>("#save-status")!;
const disconnect = document.querySelector<HTMLButtonElement>("#disconnect")!;

void initialize();

async function initialize() {
  document.querySelector<HTMLInputElement>("#node-url")!.value = await nodeURL();
  try {
    const paired = await directStatus();
    if (paired) {
      status.textContent = paired.pending ? "此設備屬於既有待配對流程，請由管理員在 NexDrop Web 核准。" : "此擴充功能已由同一節點自動信任。";
      status.className = paired.pending ? "error" : "success";
      disconnect.hidden = false;
    }
  } catch {
    status.textContent = "既有登入已失效，請重新登入並登記。";
    status.className = "error";
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
  button.disabled = true;
  status.textContent = "正在建立獨立設備…";
  try {
    const paired = await pairExtension(
      document.querySelector<HTMLInputElement>("#node-url")!.value,
      document.querySelector<HTMLInputElement>("#identifier")!.value,
      document.querySelector<HTMLInputElement>("#password")!.value,
      document.querySelector<HTMLInputElement>("#totp")!.value,
      document.querySelector<HTMLInputElement>("#device-name")!.value,
    );
    status.textContent = paired.pending ? "此設備進入相容配對流程，請到 NexDrop Web 的「設備」頁核准。" : "登記完成，已由同一節點自動信任。";
    status.className = paired.pending ? "error" : "success";
    disconnect.hidden = false;
    await chrome.runtime.sendMessage({ type: "presence_reconnect" });
  } catch (error) {
    status.textContent = messageFor(error instanceof Error ? error.message : "PAIR_FAILED", error instanceof DirectError ? error.retryAfterSeconds : undefined);
    status.className = "error";
  } finally {
    button.disabled = false;
  }
});

disconnect.addEventListener("click", async () => {
  await chrome.runtime.sendMessage({ type: "presence_disconnect" });
  await disconnectExtension();
  status.textContent = "已中斷本機連線；節點上的設備紀錄可由 NexDrop Web 撤銷。";
  status.className = "success";
  disconnect.hidden = true;
});

function messageFor(code: string, retryAfterSeconds?: number) {
  if (code === "RATE_LIMITED") return retryAfterSeconds ? `登入嘗試太頻繁，請在 ${retryAfterSeconds} 秒後再試。` : "登入嘗試太頻繁，請稍後再試。";
  const messages: Record<string, string> = { INVALID_CREDENTIALS: "帳號或密碼錯誤。", TOTP_REQUIRED: "此帳號需要六位數驗證碼。", PERMISSION_DENIED: "必須允許此節點的網站權限才能配對。", INVALID_NODE_URL: "請輸入有效的 HTTPS 節點網址。" };
  return messages[code] ?? `配對失敗：${code}`;
}
