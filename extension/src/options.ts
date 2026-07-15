import { nodeURL, normalizeNodeURL } from "./protocol.js";

const form = document.querySelector<HTMLFormElement>("form")!;
const input = document.querySelector<HTMLInputElement>("#node-url")!;
const status = document.querySelector<HTMLElement>("#save-status")!;

void nodeURL().then((value) => { input.value = value; });

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    const value = normalizeNodeURL(input.value);
    await chrome.storage.sync.set({ nodeURL: value });
    status.textContent = "設定已儲存";
    status.className = "success";
  } catch {
    status.textContent = "請輸入有效的 HTTPS 節點網址";
    status.className = "error";
  }
});
