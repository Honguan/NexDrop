import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  APIError,
  DailyTransfer,
  Device,
  DeviceStatistic,
  Overview,
  Transfer,
  User,
  api,
  statisticsPath,
} from "./api";
import { decryptFileChunks, decryptText, deviceID, encryptFiles, encryptText, ensureDeviceKey, proveDeviceSession, rememberDevice } from "./crypto";
import { messageFor } from "./errors";
import { fileMetadata, formatBytes, formatDate, labelDeviceType, statusLabel, successRate } from "./presentation";

type View = "chat" | "devices" | "analytics";
type SharedContent = { content: string; groupId: string };
const pausedTransfers = new Set<string>();
const cancelledTransfers = new Set<string>();

async function waitWhilePaused(transferId: string) {
  while (pausedTransfers.has(transferId)) await new Promise((resolve) => window.setTimeout(resolve, 250));
  if (cancelledTransfers.has(transferId)) throw new Error("傳輸已取消");
}

const navItems: Array<{ id: View; label: string; glyph: string }> = [
  { id: "chat", label: "聊天室", glyph: "◉" },
  { id: "devices", label: "設備", glyph: "▣" },
  { id: "analytics", label: "統計", glyph: "▥" },
];

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(api.hasSession());

  useEffect(() => {
    if (!api.hasSession()) return;
    api
      .get<User>("/api/account")
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <Splash />;
  if (!user) return <Login onLogin={setUser} />;
  return <Workspace user={user} onLogout={() => setUser(null)} />;
}

function Splash() {
  return (
    <main className="splash" aria-label="正在載入 NexDrop">
      <Brand />
      <span className="loader" />
    </main>
  );
}

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [needsTOTP, setNeedsTOTP] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.login(identifier, password, totp);
      onLogin(await api.get<User>("/api/account"));
    } catch (reason) {
      if (reason instanceof APIError && reason.code === "TOTP_REQUIRED") {
        setNeedsTOTP(true);
      }
      setError(messageFor(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-page">
      <section className="login-story">
        <Brand />
        <div className="story-copy">
          <p className="eyebrow">YOUR FILES. YOUR NODE.</p>
          <h1>讓每台設備，<br />都在同一條安全路徑上。</h1>
          <p>區網優先、節點接力。文字與檔案只交給你信任的設備。</p>
        </div>
        <div className="route-visual" aria-hidden="true">
          <span className="route-node node-a">W</span>
          <span className="route-line line-a" />
          <span className="route-core"><i />N</span>
          <span className="route-line line-b" />
          <span className="route-node node-b">A</span>
          <span className="route-tag">LAN FIRST</span>
        </div>
        <div className="trust-strip">
          <span><b>01</b> 端對端加密</span>
          <span><b>02</b> 自架節點</span>
          <span><b>03</b> 跨裝置同步</span>
        </div>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit}>
          <div>
            <p className="eyebrow accent">WELCOME BACK</p>
            <h2>登入 NexDrop</h2>
            <p className="muted">連線至你的私人傳輸節點</p>
          </div>
          <label>
            帳號或電子郵件
            <input autoComplete="username" value={identifier} onChange={(event) => setIdentifier(event.target.value)} placeholder="admin" required />
          </label>
          <label>
            密碼
            <input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="至少 12 個字元" required />
          </label>
          {needsTOTP && <label>
            六位數驗證碼
            <input inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" value={totp} onChange={(event) => setTotp(event.target.value)} required />
          </label>}
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary large" disabled={busy}>{busy ? "正在連線…" : "安全登入"}</button>
          <p className="login-foot">登入即表示裝置將透過 HTTPS 連線至此節點。</p>
        </form>
      </section>
    </main>
  );
}

function Workspace({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [view, setView] = useState<View>("chat");
  const [devices, setDevices] = useState<Device[]>([]);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState("");
  const [online, setOnline] = useState(false);
  const [sharedContent, setSharedContent] = useState(readSharedContent);

  const reload = useCallback(async () => {
	const [nextDevices, transferPage] = await Promise.all([
      api.get<Device[]>("/api/devices"),
	  api.get<{ items: Transfer[]; nextCursor?: string }>("/api/transfers?limit=100"),
    ]);
    setDevices(nextDevices);
	setTransfers(transferPage.items);
  }, []);

  useEffect(() => {
    const synchronize = async () => {
      const storedDeviceID = deviceID(user.id);
      if (storedDeviceID) {
        const challenge = await api.send<{ id: string; sessionId: string; ephemeralPublicKey: string; nonce: string }>(`/api/devices/${storedDeviceID}/session-challenge`, "POST");
        const proof = await proveDeviceSession(user.id, challenge.ephemeralPublicKey, challenge.nonce, challenge.sessionId);
        await api.send(`/api/devices/${storedDeviceID}/attach-session`, "POST", { challengeId: challenge.id, proof });
      }
      await reload();
    };
    synchronize().catch((reason) => setNotice(messageFor(reason))).finally(() => setLoading(false));
  }, [reload, user.id]);

  useEffect(() => {
    const refresh = window.setInterval(() => reload().catch(() => undefined), 5000);
    return () => window.clearInterval(refresh);
  }, [reload]);

  useEffect(() => {
    const localDeviceID = deviceID(user.id);
    if (!localDeviceID || !devices.some((item) => item.id === localDeviceID && item.trustStatus === "TRUSTED")) return;
    let socket: WebSocket | null = null;
    let heartbeat: number | undefined;
    let reconnect: number | undefined;
    let stopped = false;
    const connect = () => {
      const url = api.webSocketURL();
      if (!url || stopped) return;
      socket = new WebSocket(url, "nexdrop.v1");
      socket.onopen = () => setOnline(true);
      socket.onmessage = (event) => {
        const message = JSON.parse(event.data as string) as { type: string; notificationId?: string; notification?: { id: string } };
        if (message.type === "connected") {
          reload().catch(() => undefined);
          heartbeat = window.setInterval(() => socket?.send(JSON.stringify({ type: "heartbeat" })), 5000);
        }
        if (message.type === "heartbeat_ack") reload().catch(() => undefined);
        if (message.type === "notification" && message.notification) {
          socket?.send(JSON.stringify({ type: "notification_ack", notificationId: message.notification.id }));
          if ("Notification" in window) {
            if (Notification.permission === "granted") new Notification("NexDrop", { body: "收到新的訊息或資料" });
            else if (Notification.permission === "default") void Notification.requestPermission();
          }
          reload().catch(() => undefined);
        }
      };
      socket.onclose = () => {
        setOnline(false);
        if (heartbeat) window.clearInterval(heartbeat);
        if (!stopped) reconnect = window.setTimeout(connect, 3000);
      };
    };
    connect();
    return () => {
      stopped = true;
      if (heartbeat) window.clearInterval(heartbeat);
      if (reconnect) window.clearTimeout(reconnect);
      socket?.close();
    };
  }, [devices.length, reload, user.id]);

  async function logout() {
    await api.logout();
    onLogout();
  }

  const navigation = navItems;
  const content = loading ? <PanelLoader /> : (() => {
    switch (view) {
      case "chat": return <ChatView user={user} devices={devices} transfers={transfers} initialShare={sharedContent} reload={reload} onTransferCreated={(transfer) => setTransfers((current) => [transfer, ...current.filter((item) => item.id !== transfer.id)])} onSent={async () => { await reload(); setSharedContent({ content: "", groupId: "" }); }} notify={setNotice} />;
      case "devices": return <DevicesView user={user} devices={devices} reload={reload} notify={setNotice} />;
      case "analytics": return <AnalyticsView />;
    }
  })();

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        <nav aria-label="主要導覽">
          {navigation.map((item) => (
            <button key={item.id} className={view === item.id ? "active" : ""} onClick={() => setView(item.id)}>
              <span>{item.glyph}</span>{item.label}
            </button>
          ))}
        </nav>
        <div className="account-block">
          <span className="avatar">{user.username.slice(0, 1).toUpperCase()}</span>
          <span><strong>{user.username}</strong><small>{online ? "● 節點在線" : user.admin ? "節點管理員" : "成員"}</small></span>
          <button onClick={logout} aria-label="登出">↪</button>
        </div>
      </aside>
      <main className="workspace">
        <header className="mobile-header"><Brand /><button onClick={logout}>登出</button></header>
        {notice && <div className="notice" role="status"><span>{notice}</span><button onClick={() => setNotice("")}>×</button></div>}
        {content}
      </main>
      <nav className="mobile-nav" aria-label="行動版導覽">
        {navigation.slice(0, 5).map((item) => <button key={item.id} className={view === item.id ? "active" : ""} onClick={() => setView(item.id)}><span>{item.glyph}</span>{item.label}</button>)}
      </nav>
    </div>
  );
}

function ChatView({ user, devices, transfers, initialShare, reload, onTransferCreated, onSent, notify }: { user: User; devices: Device[]; transfers: Transfer[]; initialShare: SharedContent; reload: () => Promise<void>; onTransferCreated: (transfer: Transfer) => void; onSent: () => Promise<void>; notify: (value: string) => void }) {
  return <section className="chat-layout"><SendView user={user} devices={devices} initialShare={initialShare} onTransferCreated={onTransferCreated} onSent={onSent} notify={notify} /><ActivityView user={user} devices={devices} transfers={transfers} reload={reload} /></section>;
}

function SendView({ user, devices, initialShare, onTransferCreated, onSent, notify }: { user: User; devices: Device[]; initialShare: SharedContent; onTransferCreated: (transfer: Transfer) => void; onSent: () => Promise<void>; notify: (value: string) => void }) {
  const [selection, setSelection] = useState<string[] | null>(null);
  const [content, setContent] = useState(initialShare.content);
  const [files, setFiles] = useState<File[]>([]);
  const [notification, setNotification] = useState(false);
  const [busy, setBusy] = useState(false);
  const trusted = devices.filter((item) => item.trustStatus === "TRUSTED" && item.publicKey);
  const selected = selection ?? trusted.map((item) => item.id);

  function toggle(id: string) {
    setSelection(selected.includes(id) ? selected.filter((value) => value !== id) : [...selected, id]);
  }

  async function send(event: FormEvent) {
    event.preventDefault();
    if ((!content.trim() && files.length === 0) || selected.length === 0) return;
    setBusy(true);
    try {
      const recipients = trusted.filter((item) => selected.includes(item.id)).map((item) => ({ id: item.id, publicKey: item.publicKey! }));
      if (!recipients.length) throw new Error("請至少選擇一台可接收設備");
      if (files.length) {
        const encrypted = await encryptFiles(files, recipients);
        const transfer = await api.send<Transfer>("/api/transfers", "POST", {
          targetType: selected.length === 1 ? "SINGLE_DEVICE" : "MULTIPLE_DEVICES",
          targetDeviceIds: selected,
          contentType: files.every((file) => file.type.startsWith("image/")) ? "IMAGE" : "FILE",
          routeMode: "AUTOMATIC",
          allowLargeFileViaNode: true,
          content: encrypted.content,
          wrappedContentKeys: encrypted.wrappedContentKeys,
          files: encrypted.files.map((file) => file.record),
        });
        cancelledTransfers.delete(transfer.id);
        onTransferCreated(transfer);
        for (const [fileIndex, file] of encrypted.files.entries()) {
          const fileID = transfer.files[fileIndex].id;
          for (const [chunkIndex, chunk] of file.chunks.entries()) {
            await waitWhilePaused(transfer.id);
            await api.uploadChunk(`/api/files/${fileID}/chunks/${chunkIndex}`, chunk.data, chunk.sha256);
          }
          await api.send(`/api/files/${fileID}/complete`, "POST");
        }
        cancelledTransfers.delete(transfer.id);
      } else {
        const encrypted = await encryptText(content.trim(), recipients);
        await api.send<Transfer>("/api/transfers", "POST", {
        targetType: selected.length === 1 ? "SINGLE_DEVICE" : "MULTIPLE_DEVICES",
        targetDeviceIds: selected,
        contentType: notification ? "NOTIFICATION" : content.trim().startsWith("http") ? "URL" : "TEXT",
        routeMode: "AUTOMATIC",
        content: encrypted.content,
        wrappedContentKeys: encrypted.wrappedContentKeys,
        });
      }
      setContent("");
      setNotification(false);
      setFiles([]);
      setSelection(null);
      await onSent();
      notify("已建立加密傳輸任務");
    } catch (reason) {
      notify(messageFor(reason));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="page send-page">
      <PageHeading eyebrow="QUICK DROP" title="今天要傳送什麼？" description="選擇信任設備，NexDrop 會自動判斷區網或節點路徑。" />
      <div className="send-grid">
        <form className="composer card" onSubmit={send} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); const dropped = Array.from(event.dataTransfer.files); if (dropped.length) { setFiles(dropped); setContent(""); } }}>
          <div className="card-title"><span className="step">01</span><div><h3>輸入內容或選擇檔案</h3><p>內容會在瀏覽器內先加密</p></div></div>
          <textarea value={content} onChange={(event) => { setContent(event.target.value); if (event.target.value) setFiles([]); }} placeholder="貼上文字、網址或想傳給另一台設備的內容…" maxLength={100000} />
          {!files.length && <label className="check"><input type="checkbox" checked={notification} onChange={(event) => setNotification(event.target.checked)} /> 一般通知訊息</label>}
          <label className="file-input"><input type="file" multiple onChange={(event) => { setFiles(Array.from(event.target.files ?? [])); if (event.target.files?.length) setContent(""); }} /><span>＋ 選擇檔案</span><small>{files.length ? `${files.length} 個檔案 · ${formatBytes(files.reduce((total, file) => total + file.size, 0))}` : "可選擇或拖放圖片與一般檔案"}</small></label>
          <div className="composer-meta"><span>{files.length ? "檔名與內容皆加密" : `${content.length.toLocaleString()} 字元`}</span><span className="secure-pill">● 端對端加密</span></div>
          <div className="divider" />
          <div className="card-title"><span className="step">02</span><div><h3>選擇目的地</h3><p>{trusted.length ? `${trusted.length} 台信任設備可用` : "尚無信任設備"}</p></div></div>
          <div className="device-picker">
            {trusted.map((item) => (
              <button type="button" key={item.id} className={selected.includes(item.id) ? "device-option selected" : "device-option"} onClick={() => toggle(item.id)}>
                <DeviceGlyph type={item.type} /><span><strong>{item.displayName}</strong><small>{labelDeviceType(item.type)}</small></span><i>{selected.includes(item.id) ? "✓" : "+"}</i>
              </button>
            ))}
            {!trusted.length && <Empty text="請先在「設備」輸入節點密鑰並登記此設備" />}
          </div>
          <button className="primary send-button" disabled={busy || (!content.trim() && files.length === 0) || selected.length === 0}>{busy ? "正在加密與上傳…" : <>傳送給 {selected.length} 台設備 <span>↗</span></>}</button>
        </form>
        <aside className="route-card card">
          <p className="eyebrow">SMART ROUTING</p>
          <h3>路徑由當下狀態決定</h3>
          <div className="route-stack">
            <div><span className="route-icon lan">⌁</span><p><strong>同一區網</strong><small>設備直接傳輸，速度最快</small></p><b>優先</b></div>
            <div><span className="route-icon node">N</span><p><strong>不同網路</strong><small>由你的 Linux 節點安全接力</small></p></div>
            <div><span className="route-icon wait">◷</span><p><strong>大檔案離線</strong><small>保留任務，回到區網後續傳</small></p></div>
          </div>
          <div className="privacy-note"><span>◆</span><p><strong>內容不會以明文離開瀏覽器</strong><small>每個目的設備都有獨立包裝的內容金鑰。</small></p></div>
        </aside>
      </div>
    </section>
  );
}

function ActivityView({ user, devices, transfers, reload }: { user: User; devices: Device[]; transfers: Transfer[]; reload: () => Promise<void> }) {
  const [decrypted, setDecrypted] = useState<Record<string, string>>({});
  const [downloading, setDownloading] = useState("");
  const names = useMemo(() => Object.fromEntries(devices.map((item) => [item.id, item.displayName])), [devices]);

  useEffect(() => {
    const localDeviceID = deviceID(user.id);
    if (!localDeviceID) return;
    transfers.forEach((transfer) => {
      const wrapped = transfer.wrappedContentKeys?.[localDeviceID];
      if (!wrapped || !transfer.content || decrypted[transfer.id]) return;
      decryptText(user.id, transfer.content, wrapped)
        .then((value) => setDecrypted((current) => ({ ...current, [transfer.id]: value })))
        .catch(() => undefined);
    });
  }, [decrypted, transfers, user.id]);

  async function download(transfer: Transfer, fileIndex: number) {
    const localDeviceID = deviceID(user.id);
    const wrapped = localDeviceID ? transfer.wrappedContentKeys?.[localDeviceID] : undefined;
    const metadata = fileMetadata(decrypted[transfer.id], fileIndex);
    if (!wrapped || !metadata) return;
    setDownloading(transfer.files[fileIndex].id);
    try {
      const chunks: ArrayBuffer[] = [];
      for (let index = 0; index < transfer.files[fileIndex].chunkCount; index++) {
        await waitWhilePaused(transfer.id);
        chunks.push(await api.downloadChunk(`/api/files/${transfer.files[fileIndex].id}/chunks/${index}`));
      }
      const plaintext = await decryptFileChunks(user.id, wrapped, chunks);
      const url = URL.createObjectURL(new Blob(plaintext, { type: metadata.mimeType }));
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = metadata.name;
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } finally {
      setDownloading("");
    }
  }

  async function hide(transferId: string) {
    if (!confirm("要從你的傳輸紀錄刪除這則訊息嗎？接收設備已保存的副本不會被刪除。")) return;
    await api.send(`/api/transfers/${transferId}`, "DELETE");
    await reload();
  }

  async function copy(transferId: string) {
    const text = decrypted[transferId];
    if (text) await navigator.clipboard.writeText(text);
  }

  async function togglePause(transfer: Transfer) {
    const paused = transfer.status === "PAUSED";
    if (paused) pausedTransfers.delete(transfer.id); else pausedTransfers.add(transfer.id);
    await Promise.all(transfer.targets.filter((target) => !["DELIVERED", "READ", "FAILED", "CANCELLED", "EXPIRED"].includes(target.status)).map((target) => api.send(`/api/transfers/${transfer.id}/targets/${target.deviceId}`, "PUT", { status: paused ? "QUEUED" : "PAUSED", bytesTransferred: target.bytesTransferred })));
    await reload();
  }

  async function cancel(transferId: string) {
    pausedTransfers.delete(transferId);
    cancelledTransfers.add(transferId);
    await api.send(`/api/transfers/${transferId}/cancel`, "POST");
    await reload();
  }

  return (
    <section className="page">
      <PageHeading eyebrow="ACTIVITY" title="傳輸紀錄" description="最近建立與接收的任務、路徑與交付狀態。" />
      <div className="table-card card">
        <div className="table-head"><span>內容</span><span>目的地</span><span>路徑</span><span>狀態</span><span>時間</span></div>
        {transfers.map((item) => (
          <article className="table-row" key={item.id}>
            <div><span className="content-glyph">{item.files.length ? "F" : item.contentType === "URL" ? "↗" : "T"}</span><p><strong>{item.files.length ? (fileMetadata(decrypted[item.id], 0)?.name ?? "加密檔案") : decrypted[item.id] ?? (item.contentType === "TEXT" ? "加密文字" : item.contentType)}</strong><small>{item.id.slice(0, 8)}{item.files.length > 1 ? ` · ${item.files.length} 個檔案` : ""}</small>{item.files.map((file, index) => fileMetadata(decrypted[item.id], index) && <button className="text-button" key={file.id} onClick={() => download(item, index)} disabled={downloading === file.id}>{downloading === file.id ? "下載中…" : `下載 ${fileMetadata(decrypted[item.id], index)?.name}`}</button>)}</p></div>
            <span>{item.targets.map((target) => names[target.deviceId] ?? target.deviceId.slice(0, 8)).join("、")}</span>
            <span className="route-label">{item.targets[0]?.selectedRoute ?? "—"}</span>
            <Status value={item.status} />
            <div><time>{formatDate(item.createdAt)}</time>{decrypted[item.id] && <button className="text-button" onClick={() => copy(item.id)}>快速複製</button>}{item.senderUserId === user.id && !["DELIVERED", "READ", "FAILED", "CANCELLED", "EXPIRED"].includes(item.status) && <><button className="text-button" onClick={() => togglePause(item)}>{item.status === "PAUSED" ? "繼續" : "暫停"}</button><button className="text-danger" onClick={() => cancel(item.id)}>取消</button></>}<button className="text-danger" onClick={() => hide(item.id)}>刪除</button></div>
          </article>
        ))}
        {!transfers.length && <Empty text="還沒有傳輸紀錄" />}
      </div>
    </section>
  );
}

function DevicesView({ user, devices, reload, notify }: { user: User; devices: Device[]; reload: () => Promise<void>; notify: (value: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [nodeKey, setNodeKey] = useState(api.nodeKey());
  const localDevice = devices.find((item) => item.id === deviceID(user.id));
  const registered = Boolean(localDevice && localDevice.trustStatus !== "REVOKED");
  function importSettings(value: string) {
    const trimmed = value.trim();
    try { const parsed = new URL(trimmed); const key = parsed.searchParams.get("key") ?? ""; if (parsed.protocol === "nexdrop:" && parsed.hostname === "join" && key) setNodeKey(key); else setNodeKey(trimmed); } catch { setNodeKey(trimmed); }
  }
  async function register() {
    api.setNodeKey(nodeKey); setBusy(true);
    try {
      const keys = await ensureDeviceKey(user.id);
      const created = await api.send<Device>("/api/devices", "POST", { displayName: browserName(), type: navigator.userAgent.includes("Edg/") ? "WEB_EDGE" : "WEB_CHROME", publicKey: keys.publicKey, keyAlgorithm: "X25519" });
      rememberDevice(user.id, created.id); await reload(); notify("設備已使用節點密鑰加入");
    } catch (reason) { notify(messageFor(reason)); } finally { setBusy(false); }
  }
  async function revoke(id: string) { try { await api.send(`/api/devices/${id}/revoke`, "POST"); await reload(); notify("設備已撤銷"); } catch (reason) { notify(messageFor(reason)); } }
  const importValue = `nexdrop://join?node=${encodeURIComponent(location.origin)}&key=${encodeURIComponent(nodeKey)}`;
  return <section className="page">
    <PageHeading eyebrow="NODE DEVICES" title="設備" description="設備只需要節點連結與節點密鑰即可加入；同一節點不再使用配對碼。" action={<button className="primary" onClick={register} disabled={busy || registered || !nodeKey.trim()}>{busy ? "加入中…" : registered ? "此瀏覽器已加入" : "+ 加入此瀏覽器"}</button>} />
    <div className="card settings-form"><div className="list-title"><div><p className="eyebrow">NODE IMPORT</p><h3>節點連結與密鑰</h3></div><button className="secondary" type="button" onClick={() => navigator.clipboard.writeText(importValue)}>一鍵複製導入</button></div><label>節點連結<input readOnly value={location.origin} /></label><label>節點密鑰<input type="password" value={nodeKey} onChange={(event) => importSettings(event.target.value)} placeholder="貼上節點密鑰或完整 nexdrop://join 資料" /></label><label>完整導入資料<input value={importValue} onChange={(event) => importSettings(event.target.value)} onFocus={(event) => event.currentTarget.select()} /></label></div>
    <div className="cards-grid devices-grid">{devices.map((item) => <article className="device-card card" key={item.id}><div className="device-top"><DeviceGlyph type={item.type} /><Status value={item.online ? "ONLINE" : "OFFLINE"} /></div><h3>{item.displayName}</h3><p>{labelDeviceType(item.type)} · {item.online ? "目前在線" : item.lastSeenAt ? `最後上線 ${formatDate(item.lastSeenAt)}` : "尚未連線"}</p><div className="device-actions"><Status value={item.trustStatus} />{item.trustStatus !== "REVOKED" && <button className="text-danger" onClick={() => revoke(item.id)}>撤銷</button>}</div></article>)}{!devices.length && <Empty text="尚未加入任何設備" />}</div>
  </section>;
}

function AnalyticsView() {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [daily, setDaily] = useState<DailyTransfer[]>([]);
  const [deviceStats, setDeviceStats] = useState<DeviceStatistic[]>([]);
  const [error, setError] = useState("");
  const [range, setRange] = useState({ preset: "7", from: "", to: "" });
  const load = useCallback(async () => {
    setError("");
    const path = (endpoint: string) => range.preset === "custom" && range.from && range.to
      ? `${endpoint}?${new URLSearchParams({ from: new Date(`${range.from}T00:00:00`).toISOString(), to: new Date(`${range.to}T23:59:59.999`).toISOString() })}`
      : statisticsPath(endpoint, Number(range.preset));
    try {
      const [nextOverview, nextDaily, nextDevices] = await Promise.all([
        api.get<Overview>(path("/api/statistics/overview")), api.get<DailyTransfer[]>(path("/api/statistics/transfers")),
        api.get<DeviceStatistic[]>(path("/api/statistics/devices")),
      ]);
      setOverview(nextOverview); setDaily(nextDaily); setDeviceStats(nextDevices);
    } catch (reason) { setError(messageFor(reason)); }
  }, [range]);
  useEffect(() => {
    void load();
    const refresh = window.setInterval(() => void load(), 5000);
    return () => window.clearInterval(refresh);
  }, [load]);
  const peak = Math.max(1, ...daily.map((item) => item.totalBytes));
  return (
    <section className="page">
      <PageHeading eyebrow="TRANSFER ANALYTICS" title="傳輸統計" description="掌握流量、成功率與實際使用的傳輸路徑。" action={<div className="admin-tabs"><select value={range.preset} onChange={(event) => setRange({ ...range, preset: event.target.value })}><option value="1">24 小時</option><option value="7">7 天</option><option value="30">30 天</option><option value="90">90 天</option><option value="custom">自訂</option></select>{range.preset === "custom" && <><input type="date" value={range.from} onChange={(event) => setRange({ ...range, from: event.target.value })} /><input type="date" value={range.to} onChange={(event) => setRange({ ...range, to: event.target.value })} /></>}</div>} />
      {!!daily.length && <div className="card audit-list"><div className="list-title"><h3>每日傳輸明細</h3><span>{daily.length} 天</span></div>{daily.map((item) => <article key={item.date}><span className="audit-mark">▥</span><p><strong>{item.date} · {item.count} 次</strong><small>總計 {formatBytes(item.totalBytes)} · LAN {formatBytes(item.lanBytes)} · 節點 {formatBytes(item.nodeBytes)}</small></p><Status value={item.failed ? "FAILED" : "DELIVERED"} /></article>)}</div>}
      {!!deviceStats.length && <div className="card audit-list"><div className="list-title"><h3>每台設備狀態與傳輸用量</h3><span>{deviceStats.filter((item) => item.online).length}／{deviceStats.length} 台在線</span></div>{deviceStats.map((item) => <article key={item.deviceId}><DeviceGlyph type={item.deviceType} /><p><strong>{item.displayName}</strong><small>{labelDeviceType(item.deviceType)} · 傳送 {item.sentCount} 筆／{formatBytes(item.sentBytes)} · 接收 {item.receivedCount} 筆／{formatBytes(item.receivedBytes)} · {item.online ? "即時在線" : item.lastSeenAt ? `最後上線 ${formatDate(item.lastSeenAt)}` : "尚未連線"}</small></p><Status value={item.online ? "ONLINE" : "OFFLINE"} /></article>)}</div>}
      {error ? <Empty text={error} /> : !overview ? <PanelLoader /> : <><div className="metric-grid"><Metric label="傳輸任務" value={overview.transferCount.toLocaleString()} note="選定期間" /><Metric label="傳輸容量" value={formatBytes(overview.totalBytes)} note="全部設備" /><Metric label="成功交付" value={overview.succeeded.toLocaleString()} note={`${successRate(overview)}% 成功率`} /><Metric label="失敗" value={overview.failed.toLocaleString()} note="可於紀錄中追蹤" danger={overview.failed > 0} /></div><div className="admin-layout"><div className="card route-summary"><div><p className="eyebrow">DAILY TREND</p><h3>每日流量</h3></div>{daily.map((item) => <div className="route-bar" key={item.date}><span>{item.date.slice(5)}</span><i><b style={{ width: `${Math.max(2, item.totalBytes / peak * 100)}%` }} /></i><strong>{formatBytes(item.totalBytes)}</strong></div>)}{!daily.length && <Empty text="尚無每日資料" />}</div><div className="card route-summary"><div><p className="eyebrow">ROUTE MIX</p><h3>傳輸路徑分布</h3></div>{Object.entries(overview.routeCounts ?? {}).map(([route, count]) => <div className="route-bar" key={route}><span>{route}</span><i><b style={{ width: `${Math.max(4, (count / Math.max(overview.transferCount, 1)) * 100)}%` }} /></i><strong>{count}</strong></div>)}{!Object.keys(overview.routeCounts ?? {}).length && <Empty text="尚無足夠資料" />}</div></div></>}
    </section>
  );
}

function PageHeading({ eyebrow, title, description, action }: { eyebrow: string; title: string; description: string; action?: React.ReactNode }) {
  return <header className="page-heading"><div><p className="eyebrow accent">{eyebrow}</p><h1>{title}</h1><p>{description}</p></div>{action}</header>;
}
function Brand() { return <div className="brand"><span className="brand-mark"><i /><b>N</b></span><strong>NexDrop</strong></div>; }
function DeviceGlyph({ type }: { type: string }) { return <span className={`device-glyph ${type.toLowerCase()}`}>{type.includes("ANDROID") ? "A" : type.includes("WEB") ? "W" : "▰"}</span>; }
function Status({ value }: { value: string }) { return <span className={`status status-${value.toLowerCase().replaceAll("_", "-")}`}>{statusLabel(value)}</span>; }
function Empty({ text }: { text: string }) { return <div className="empty"><span>◇</span><p>{text}</p></div>; }
function PanelLoader() { return <div className="panel-loader"><span className="loader" /><p>正在同步節點資料…</p></div>; }
function Metric({ label, value, note, danger }: { label: string; value: string; note: string; danger?: boolean }) { return <article className={`metric card ${danger ? "danger" : ""}`}><p>{label}</p><strong>{value}</strong><small>{note}</small></article>; }

function browserName() { return `${navigator.userAgent.includes("Edg/") ? "Edge" : "Chrome"} · ${navigator.platform || "Web"}`; }
function readSharedContent() {
  if (!location.hash.startsWith("#share=")) return { content: "", groupId: "" };
  try {
    const encoded = location.hash.slice(7).replaceAll("-", "+").replaceAll("_", "/");
    const padded = encoded.padEnd(Math.ceil(encoded.length / 4) * 4, "=");
    const binary = atob(padded);
    const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const payload = JSON.parse(new TextDecoder().decode(bytes)) as { kind?: string; title?: string; url?: string; text?: string; groupId?: string };
    history.replaceState(null, "", `${location.pathname}${location.search}`);
    const content = payload.kind === "SELECTION" ? [payload.text, payload.url].filter(Boolean).join("\n\n") : payload.url ?? payload.text ?? "";
    return { content, groupId: payload.groupId ?? "" };
  } catch {
    history.replaceState(null, "", `${location.pathname}${location.search}`);
    return { content: "", groupId: "" };
  }
}
