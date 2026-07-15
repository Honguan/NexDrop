import { SharePayload, NativeResponse, nodeURL, requestID, requestNative } from "./protocol.js";

const statusElement = document.querySelector<HTMLElement>("#status")!;
const targetsElement = document.querySelector<HTMLElement>("#targets")!;
const sendButton = document.querySelector<HTMLButtonElement>("#send")!;
const openButton = document.querySelector<HTMLButtonElement>("#open-web")!;
let status: NonNullable<NativeResponse["status"]> | null = null;

void initialize();

async function initialize() {
  openButton.addEventListener("click", async () => chrome.tabs.create({ url: await nodeURL() }));
  sendButton.addEventListener("click", sendCurrentPage);
  try {
    const response = await requestNative({ id: requestID(), type: "status" });
    if (!response.ok || !response.status) throw new Error(response.error ?? "UNAVAILABLE");
    status = response.status;
    renderStatus();
  } catch {
    statusElement.textContent = "Desktop 未連線";
    statusElement.classList.add("offline");
    targetsElement.innerHTML = '<p class="empty">傳送時將開啟 NexDrop Web</p>';
    sendButton.textContent = "在 Web 中傳送";
    sendButton.disabled = false;
  }
}

function renderStatus() {
  statusElement.textContent = status?.connected ? "Desktop 已連線" : "節點離線";
  statusElement.classList.toggle("offline", !status?.connected);
  const targets = status?.devices ?? [];
  const groups = status?.groups ?? [];
  const deviceNodes = targets.map((device) => {
    const label = document.createElement("label");
    label.className = "target";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.value = device.id;
    input.disabled = !device.online;
    input.addEventListener("change", () => {
      const selectedGroup = document.querySelector<HTMLInputElement>('#targets input[name="group"]:checked');
      if (input.checked && selectedGroup) selectedGroup.checked = false;
    });
    const text = document.createElement("span");
    text.innerHTML = `<strong>${escapeHTML(device.name)}</strong><small>${device.online ? "在線" : "離線"}</small>`;
    label.append(input, text);
    return label;
  });
  const groupNodes = groups.map((group) => {
    const label = document.createElement("label");
    label.className = "target group";
    const input = document.createElement("input");
    input.type = "radio";
    input.name = "group";
    input.value = group.id;
    input.addEventListener("change", () => {
      if (input.checked) document.querySelectorAll<HTMLInputElement>('#targets input[type="checkbox"]:checked').forEach((item) => { item.checked = false; });
    });
    const text = document.createElement("span");
    text.innerHTML = `<strong>${escapeHTML(group.name)}</strong><small>群組全部設備</small>`;
    label.append(input, text);
    return label;
  });
  const nodes: Node[] = [];
  if (deviceNodes.length) nodes.push(sectionLabel("設備"), ...deviceNodes);
  if (groupNodes.length) nodes.push(sectionLabel("群組"), ...groupNodes);
  targetsElement.replaceChildren(...nodes);
  if (!nodes.length) targetsElement.innerHTML = '<p class="empty">尚無可用設備或群組</p>';
  sendButton.disabled = false;
}

async function sendCurrentPage() {
  sendButton.disabled = true;
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const payload: SharePayload = {
    kind: "PAGE",
    title: tab?.title,
    url: tab?.url,
    targetDeviceIds: [...document.querySelectorAll<HTMLInputElement>('#targets input[type="checkbox"]:checked')].map((item) => item.value),
    groupId: document.querySelector<HTMLInputElement>('#targets input[name="group"]:checked')?.value,
  };
  const response = await chrome.runtime.sendMessage({ type: "share", payload });
  sendButton.textContent = response?.fallback ? "已開啟 Web" : response?.ok ? "已送出" : "傳送失敗";
  window.setTimeout(() => window.close(), 700);
}

function sectionLabel(value: string) {
  const label = document.createElement("p");
  label.className = "section-label";
  label.textContent = value;
  return label;
}

function escapeHTML(value: string) {
  return value.replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]!);
}
