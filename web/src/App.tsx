import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AdminDevice,
  AdminFailure,
  AdminUser,
  APIError,
  AuditLog,
  DailyTransfer,
  Device,
  DeviceStatistic,
  Invitation,
  NodeMetric,
  NodeSettings,
  Overview,
  StorageOverview,
  Transfer,
  User,
  api,
  statisticsPath,
} from "./api";
import { decryptFileChunks, decryptText, deviceID, encryptFiles, encryptText, ensureDeviceKey, proveDeviceSession, rememberDevice } from "./crypto";
import { rateLimitMessage } from "./errors";

type View = "send" | "activity" | "devices" | "analytics" | "admin";
type SharedContent = { content: string; groupId: string };
const pausedTransfers = new Set<string>();
const cancelledTransfers = new Set<string>();

async function waitWhilePaused(transferId: string) {
  while (pausedTransfers.has(transferId)) await new Promise((resolve) => window.setTimeout(resolve, 250));
  if (cancelledTransfers.has(transferId)) throw new Error("傳輸已取消");
}

const navItems: Array<{ id: View; label: string; glyph: string }> = [
  { id: "send", label: "傳送", glyph: "↗" },
  { id: "activity", label: "傳輸紀錄", glyph: "◷" },
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
  const [invitationToken, setInvitationToken] = useState(() => new URLSearchParams(location.search).get("invite") ?? "");
  const [invitationPassword, setInvitationPassword] = useState({ password: "", confirmation: "" });
  const [invitationAccepted, setInvitationAccepted] = useState(false);

  async function acceptInvitation(event: FormEvent) {
    event.preventDefault();
    if (invitationPassword.password !== invitationPassword.confirmation) {
      setError("兩次輸入的密碼不一致");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const accepted = await api.acceptInvitation(invitationToken, invitationPassword.password);
      history.replaceState(null, "", location.pathname);
      setIdentifier(accepted.username);
      setInvitationToken("");
      setInvitationAccepted(true);
    } catch (reason) {
      setError(messageFor(reason));
    } finally {
      setBusy(false);
    }
  }

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

  if (invitationToken) return (
    <main className="login-page">
      <section className="login-story"><Brand /><div className="story-copy"><p className="eyebrow">ACCOUNT INVITATION</p><h1>完成你的<br />NexDrop 帳號。</h1><p>設定專屬密碼後，即可登入私人傳輸節點。</p></div></section>
      <section className="login-panel"><form className="login-card" onSubmit={acceptInvitation}><div><p className="eyebrow">INVITATION</p><h2>接受帳號邀請</h2><p className="muted">邀請連結僅能使用一次，並會在七天後失效。</p></div><label>設定密碼<input type="password" autoComplete="new-password" minLength={12} value={invitationPassword.password} onChange={(event) => setInvitationPassword({ ...invitationPassword, password: event.target.value })} required /></label><label>再次輸入密碼<input type="password" autoComplete="new-password" minLength={12} value={invitationPassword.confirmation} onChange={(event) => setInvitationPassword({ ...invitationPassword, confirmation: event.target.value })} required /></label>{error && <p className="form-error" role="alert">{error}</p>}<button className="primary large" disabled={busy}>{busy ? "正在建立帳號…" : "接受邀請"}</button></form></section>
    </main>
  );

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
          {invitationAccepted && <p className="muted" role="status">邀請已接受，請使用新密碼登入。</p>}
          <button className="primary large" disabled={busy}>{busy ? "正在連線…" : "安全登入"}</button>
          <p className="login-foot">登入即表示裝置將透過 HTTPS 連線至此節點。</p>
        </form>
      </section>
    </main>
  );
}

function Workspace({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [view, setView] = useState<View>("send");
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

  const navigation = user.admin ? [...navItems, { id: "admin" as View, label: "管理後台", glyph: "◆" }] : navItems;
  const content = loading ? <PanelLoader /> : (() => {
    switch (view) {
      case "send": return <SendView user={user} devices={devices} initialShare={sharedContent} onTransferCreated={(transfer) => { setTransfers((current) => [transfer, ...current.filter((item) => item.id !== transfer.id)]); setView("activity"); }} onSent={async () => { await reload(); setSharedContent({ content: "", groupId: "" }); }} notify={setNotice} />;
      case "activity": return <ActivityView user={user} devices={devices} transfers={transfers} reload={reload} />;
      case "devices": return <DevicesView user={user} devices={devices} reload={reload} notify={setNotice} />;
      case "analytics": return <AnalyticsView user={user} />;
      case "admin": return <AdminView user={user} notify={setNotice} />;
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
        <form className="composer card" onSubmit={send}>
          <div className="card-title"><span className="step">01</span><div><h3>輸入內容或選擇檔案</h3><p>內容會在瀏覽器內先加密</p></div></div>
          <textarea value={content} onChange={(event) => { setContent(event.target.value); if (event.target.value) setFiles([]); }} placeholder="貼上文字、網址或想傳給另一台設備的內容…" maxLength={100000} />
          {!files.length && <label className="check"><input type="checkbox" checked={notification} onChange={(event) => setNotification(event.target.checked)} /> 一般通知訊息</label>}
          <label className="file-input"><input type="file" multiple onChange={(event) => { setFiles(Array.from(event.target.files ?? [])); if (event.target.files?.length) setContent(""); }} /><span>＋ 選擇檔案</span><small>{files.length ? `${files.length} 個檔案 · ${formatBytes(files.reduce((total, file) => total + file.size, 0))}` : "圖片與一般檔案皆可"}</small></label>
          <div className="composer-meta"><span>{files.length ? "檔名與內容皆加密" : `${content.length.toLocaleString()} 字元`}</span><span className="secure-pill">● 端對端加密</span></div>
          <div className="divider" />
          <div className="card-title"><span className="step">02</span><div><h3>選擇目的地</h3><p>{trusted.length ? `${trusted.length} 台信任設備可用` : "尚無信任設備"}</p></div></div>
          <div className="device-picker">
            {trusted.map((item) => (
              <button type="button" key={item.id} className={selected.includes(item.id) ? "device-option selected" : "device-option"} onClick={() => toggle(item.id)}>
                <DeviceGlyph type={item.type} /><span><strong>{item.displayName}</strong><small>{labelDeviceType(item.type)}</small></span><i>{selected.includes(item.id) ? "✓" : "+"}</i>
              </button>
            ))}
            {!trusted.length && <Empty text="請前往「設備」查看此設備自動產生的配對碼" />}
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
  const [pairing, setPairing] = useState<{ id: string; code: string; qrPayload: string; expiresAt: string } | null>(null);
  const [pairingInput, setPairingInput] = useState({ challengeId: "", code: "", payload: "" });
  const pairingRequested = useRef(false);
  const localDevice = devices.find((item) => item.id === deviceID(user.id));
  const registered = Boolean(localDevice && localDevice.trustStatus !== "REVOKED");

  async function register() {
    setBusy(true);
    try {
      const keys = await ensureDeviceKey(user.id);
      const created = await api.send<Device>("/api/devices", "POST", {
        displayName: browserName(), type: navigator.userAgent.includes("Edg/") ? "WEB_EDGE" : "WEB_CHROME",
        publicKey: keys.publicKey, keyAlgorithm: "X25519",
      });
      rememberDevice(user.id, created.id);
      await reload();
      notify(created.trustStatus === "TRUSTED" ? "此瀏覽器已由同一節點自動信任" : "此瀏覽器已產生配對碼");
    } catch (reason) { notify(messageFor(reason)); } finally { setBusy(false); }
  }

  const createPairing = useCallback(async () => {
    if (!localDevice || localDevice.trustStatus !== "PENDING") return;
    setBusy(true);
    try {
      setPairing(await api.send<{ id: string; code: string; qrPayload: string; expiresAt: string }>(`/api/devices/${localDevice.id}/pairing-code`, "POST"));
    } catch (reason) {
      pairingRequested.current = false;
      notify(messageFor(reason));
    } finally {
      setBusy(false);
    }
  }, [localDevice?.id, localDevice?.trustStatus, notify]);

  useEffect(() => {
    if (!localDevice || localDevice.trustStatus !== "PENDING" || pairingRequested.current) return;
    pairingRequested.current = true;
    void createPairing();
  }, [createPairing, localDevice]);

  function readPairingPayload(value: string) {
    const parsed = new URL(value.trim());
    if (parsed.protocol !== "nexdrop:" || parsed.hostname !== "pair") return;
    setPairingInput({ payload: value, challengeId: parsed.searchParams.get("id") ?? "", code: parsed.searchParams.get("code") ?? "" });
  }

  async function pair(event: FormEvent) {
    event.preventDefault();
    if (!localDevice || localDevice.trustStatus !== "TRUSTED") return;
    setBusy(true);
    try {
      await api.send(`/api/devices/${localDevice.id}/pair`, "POST", {
        challengeId: pairingInput.challengeId.trim(),
        code: pairingInput.code.trim(),
      });
      setPairingInput({ challengeId: "", code: "", payload: "" });
      await reload();
      notify("新設備已完成配對");
    } catch (reason) { notify(messageFor(reason)); } finally { setBusy(false); }
  }

  async function revoke(id: string) {
    try { await api.send(`/api/devices/${id}/revoke`, "POST"); await reload(); notify("設備已撤銷"); } catch (reason) { notify(messageFor(reason)); }
  }

  return (
    <section className="page">
      <PageHeading eyebrow="TRUSTED DEVICES" title="設備" description="待核准設備會自動產生配對碼，再由任一已信任設備完成核准。" action={<button className="primary" onClick={register} disabled={busy || registered}>{busy ? "建立中…" : registered ? "此瀏覽器已登記" : "+ 登記此瀏覽器"}</button>} />
      {localDevice?.trustStatus === "PENDING" && <div className="card settings-form">
        <div className="list-title"><div><p className="eyebrow">PAIR THIS DEVICE</p><h3>此設備配對碼</h3></div><button className="secondary" disabled={busy} onClick={() => { pairingRequested.current = true; void createPairing(); }}>重新產生</button></div>
        {pairing ? <><label>6 位數配對碼<input readOnly value={pairing.code} onFocus={(event) => event.currentTarget.select()} /></label><label>挑戰 ID<input readOnly value={pairing.id} onFocus={(event) => event.currentTarget.select()} /></label><label>完整配對資料<input readOnly value={pairing.qrPayload} onFocus={(event) => event.currentTarget.select()} /></label><small>請在 10 分鐘內，於另一台已信任設備的「核准新設備」輸入以上資料。</small></> : <PanelLoader />}
      </div>}
      {localDevice?.trustStatus === "TRUSTED" && <form className="card settings-form" onSubmit={pair}>
        <div className="list-title"><div><p className="eyebrow">APPROVE DEVICE</p><h3>核准新設備</h3></div><button className="primary" disabled={busy}>完成配對</button></div>
        <label>貼上新設備的完整配對資料<input value={pairingInput.payload} onChange={(event) => { try { readPairingPayload(event.target.value); } catch { setPairingInput({ ...pairingInput, payload: event.target.value }); } }} placeholder="nexdrop://pair?..." /></label>
        <div className="settings-grid"><label>挑戰 ID<input value={pairingInput.challengeId} onChange={(event) => setPairingInput({ ...pairingInput, challengeId: event.target.value })} required /></label><label>6 位數配對碼<input inputMode="numeric" pattern="[0-9]{6}" value={pairingInput.code} onChange={(event) => setPairingInput({ ...pairingInput, code: event.target.value.replace(/\D/g, "").slice(0, 6) })} required /></label></div>
      </form>}
      <div className="cards-grid devices-grid">
        {devices.map((item) => <article className="device-card card" key={item.id}><div className="device-top"><DeviceGlyph type={item.type} /><Status value={item.online ? "ONLINE" : "OFFLINE"} /></div><h3>{item.displayName}</h3><p>{labelDeviceType(item.type)} · {item.online ? "目前在線" : item.lastSeenAt ? `最後上線 ${formatDate(item.lastSeenAt)}` : "尚未連線"}</p><div className="device-actions"><Status value={item.trustStatus} />{item.trustStatus !== "REVOKED" && <button className="text-danger" onClick={() => revoke(item.id)}>撤銷</button>}</div></article>)}
        {!devices.length && <Empty text="尚未登記任何設備" />}
      </div>
    </section>
  );
}

function AnalyticsView({ user }: { user: User }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [daily, setDaily] = useState<DailyTransfer[]>([]);
  const [deviceStats, setDeviceStats] = useState<DeviceStatistic[]>([]);
  const [nodeStats, setNodeStats] = useState<NodeMetric[]>([]);
  const [error, setError] = useState("");
  const [range, setRange] = useState({ preset: "7", from: "", to: "" });
  const load = useCallback(async () => {
    setError("");
    const path = (endpoint: string) => range.preset === "custom" && range.from && range.to
      ? `${endpoint}?${new URLSearchParams({ from: new Date(`${range.from}T00:00:00`).toISOString(), to: new Date(`${range.to}T23:59:59.999`).toISOString() })}`
      : statisticsPath(endpoint, Number(range.preset));
    try {
      const [nextOverview, nextDaily, nextDevices, nextNode] = await Promise.all([
        api.get<Overview>(path("/api/statistics/overview")), api.get<DailyTransfer[]>(path("/api/statistics/transfers")),
        api.get<DeviceStatistic[]>(path("/api/statistics/devices")),
        user.admin ? api.get<NodeMetric[]>(currentNodeStatisticsPath()) : Promise.resolve([]),
      ]);
      setOverview(nextOverview); setDaily(nextDaily); setDeviceStats(nextDevices); setNodeStats(nextNode);
    } catch (reason) { setError(messageFor(reason)); }
  }, [range, user.admin]);
  useEffect(() => {
    void load();
    const refresh = window.setInterval(() => void load(), 5000);
    return () => window.clearInterval(refresh);
  }, [load]);
  const peak = Math.max(1, ...daily.map((item) => item.totalBytes));
  const latestNode = nodeStats.at(-1);
  return (
    <section className="page">
      <PageHeading eyebrow="TRANSFER ANALYTICS" title="傳輸統計" description="掌握流量、成功率與實際使用的傳輸路徑。" action={<div className="admin-tabs"><select value={range.preset} onChange={(event) => setRange({ ...range, preset: event.target.value })}><option value="1">24 小時</option><option value="7">7 天</option><option value="30">30 天</option><option value="90">90 天</option><option value="custom">自訂</option></select>{range.preset === "custom" && <><input type="date" value={range.from} onChange={(event) => setRange({ ...range, from: event.target.value })} /><input type="date" value={range.to} onChange={(event) => setRange({ ...range, to: event.target.value })} /></>}</div>} />
      {!!daily.length && <div className="card audit-list"><div className="list-title"><h3>每日傳輸明細</h3><span>{daily.length} 天</span></div>{daily.map((item) => <article key={item.date}><span className="audit-mark">▥</span><p><strong>{item.date} · {item.count} 次</strong><small>總計 {formatBytes(item.totalBytes)} · LAN {formatBytes(item.lanBytes)} · 節點 {formatBytes(item.nodeBytes)}</small></p><Status value={item.failed ? "FAILED" : "DELIVERED"} /></article>)}</div>}
      {!!deviceStats.length && <div className="card audit-list"><div className="list-title"><h3>每台設備狀態與傳輸用量</h3><span>{deviceStats.filter((item) => item.online).length}／{deviceStats.length} 台在線</span></div>{deviceStats.map((item) => <article key={item.deviceId}><DeviceGlyph type={item.deviceType} /><p><strong>{item.displayName}</strong><small>{labelDeviceType(item.deviceType)} · 傳送 {item.sentCount} 筆／{formatBytes(item.sentBytes)} · 接收 {item.receivedCount} 筆／{formatBytes(item.receivedBytes)} · {item.online ? "即時在線" : item.lastSeenAt ? `最後上線 ${formatDate(item.lastSeenAt)}` : "尚未連線"}</small></p><Status value={item.online ? "ONLINE" : "OFFLINE"} /></article>)}</div>}
      {error ? <Empty text={error} /> : !overview ? <PanelLoader /> : <><div className="metric-grid"><Metric label="傳輸任務" value={overview.transferCount.toLocaleString()} note="選定期間" /><Metric label="傳輸容量" value={formatBytes(overview.totalBytes)} note="全部設備" /><Metric label="成功交付" value={overview.succeeded.toLocaleString()} note={`${successRate(overview)}% 成功率`} /><Metric label="失敗" value={overview.failed.toLocaleString()} note="可於紀錄中追蹤" danger={overview.failed > 0} /></div><div className="admin-layout"><div className="card route-summary"><div><p className="eyebrow">DAILY TREND</p><h3>每日流量</h3></div>{daily.map((item) => <div className="route-bar" key={item.date}><span>{item.date.slice(5)}</span><i><b style={{ width: `${Math.max(2, item.totalBytes / peak * 100)}%` }} /></i><strong>{formatBytes(item.totalBytes)}</strong></div>)}{!daily.length && <Empty text="尚無每日資料" />}</div><div className="card route-summary"><div><p className="eyebrow">ROUTE MIX</p><h3>傳輸路徑分布</h3></div>{Object.entries(overview.routeCounts ?? {}).map(([route, count]) => <div className="route-bar" key={route}><span>{route}</span><i><b style={{ width: `${Math.max(4, (count / Math.max(overview.transferCount, 1)) * 100)}%` }} /></i><strong>{count}</strong></div>)}{!Object.keys(overview.routeCounts ?? {}).length && <Empty text="尚無足夠資料" />}</div></div>{latestNode && <div className="metric-grid"><Metric label="節點狀態" value="在線" note={`最後更新 ${formatDate(latestNode.recordedAt)}`} /><Metric label="在線設備" value={latestNode.onlineDevices.toLocaleString()} note={`${latestNode.activeTransfers} 筆進行中`} /><Metric label="節點儲存" value={formatBytes(latestNode.diskBytes)} note={`快取 ${formatBytes(latestNode.cacheBytes)}`} /><Metric label="節點流量" value={formatBytes(latestNode.networkUploadBytes + latestNode.networkDownloadBytes)} note={`上傳 ${formatBytes(latestNode.networkUploadBytes)}`} /></div>}</>}
    </section>
  );
}

function AdminView({ user, notify }: { user: User; notify: (value: string) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [adminDevices, setAdminDevices] = useState<AdminDevice[]>([]);
  const [storage, setStorage] = useState<StorageOverview | null>(null);
  const [settings, setSettings] = useState<NodeSettings | null>(null);
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [failures, setFailures] = useState<AdminFailure[]>([]);
  const [nodeMetrics, setNodeMetrics] = useState<NodeMetric[]>([]);
  const [tab, setTab] = useState<"users" | "resources" | "node" | "audit">("users");
  const [newUser, setNewUser] = useState({ username: "", email: "", password: "", admin: false });
  const [inviteMode, setInviteMode] = useState(false);
  const [invitationLink, setInvitationLink] = useState("");
  const [verified, setVerified] = useState(false);
  const [totpReady, setTOTPReady] = useState(user.totpEnabled);
  const [verification, setVerification] = useState({ password: "", code: "" });
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
  const [adminLoading, setAdminLoading] = useState(false);
  const [adminLoadError, setAdminLoadError] = useState("");
  const settingsInitialized = useRef(false);

  const load = useCallback(async () => {
    setAdminLoading(true);
    setAdminLoadError("");
    const labels = ["使用者", "裝置", "儲存", "節點設定", "稽核紀錄", "失敗紀錄", "節點指標"];
    try {
      const results = await Promise.allSettled([
        loadAdminPages<AdminUser>("/api/admin/users"),
        loadAdminPages<AdminDevice>("/api/admin/devices"),
        api.get<StorageOverview>("/api/admin/storage"),
        api.get<NodeSettings>("/api/admin/settings"),
        api.get<AuditLog[]>("/api/admin/audit-logs"),
        api.get<AdminFailure[]>("/api/admin/failures"),
        api.get<NodeMetric[]>(currentNodeStatisticsPath()),
      ]);
      if (results[0].status === "fulfilled") setUsers(results[0].value as AdminUser[]);
      if (results[1].status === "fulfilled") setAdminDevices(results[1].value as AdminDevice[]);
      if (results[2].status === "fulfilled") setStorage(results[2].value as StorageOverview);
      if (results[3].status === "fulfilled" && !settingsInitialized.current) {
        settingsInitialized.current = true;
        setSettings(results[3].value as NodeSettings);
      }
      if (results[4].status === "fulfilled") setLogs(results[4].value as AuditLog[]);
      if (results[5].status === "fulfilled") setFailures(results[5].value as AdminFailure[]);
      if (results[6].status === "fulfilled") setNodeMetrics(results[6].value as NodeMetric[]);
      const unavailable = results.flatMap((result, index) => result.status === "rejected" ? [labels[index]] : []);
      if (unavailable.length) {
        const message = `部分管理資料暫時無法載入：${unavailable.join("、")}`;
        setAdminLoadError(message);
        notify(message);
      }
    } finally {
      setAdminLoading(false);
    }
  }, [notify]);
  useEffect(() => {
    if (!verified) return;
    void load();
    const refresh = window.setInterval(() => void load(), 30_000);
    return () => window.clearInterval(refresh);
  }, [load, verified]);
  async function beginSetup(event: FormEvent) {
    event.preventDefault();
    try { setSetup(await api.send<{ secret: string; uri: string }>("/api/auth/totp/setup", "POST", { password: verification.password })); } catch (reason) { notify(messageFor(reason)); }
  }
  async function enableTOTP(event: FormEvent) {
    event.preventDefault();
    if (!setup) return;
    try { await api.send("/api/auth/totp/enable", "POST", { password: verification.password, secret: setup.secret, code: verification.code }); setTOTPReady(true); setSetup(null); setVerification({ ...verification, code: "" }); notify("TOTP 已啟用"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function verify(event: FormEvent) {
    event.preventDefault();
    try { await api.send("/api/auth/admin-verify", "POST", { password: verification.password, totp: verification.code }); setVerified(true); setVerification({ password: "", code: "" }); } catch (reason) { notify(messageFor(reason)); }
  }
  async function createUser(event: FormEvent) {
    event.preventDefault();
    try {
      if (inviteMode) {
        const invitation = await api.send<Invitation>("/api/admin/invitations", "POST", { username: newUser.username, email: newUser.email, admin: newUser.admin });
        setInvitationLink(`${location.origin}${location.pathname}?invite=${encodeURIComponent(invitation.token)}`);
        notify("一次性邀請連結已建立");
      } else {
        await api.send("/api/admin/users", "POST", newUser);
        setInvitationLink("");
        await load();
        notify("使用者帳號已建立");
      }
      setNewUser({ username: "", email: "", password: "", admin: false });
    } catch (reason) { notify(messageFor(reason)); }
  }
  async function disable(id: string) {
    try { await api.send(`/api/admin/users/${id}`, "DELETE"); await load(); notify("使用者已停用"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function revokeDevice(id: string) {
    if (!confirm("確定要撤銷此裝置並登出其工作階段？")) return;
    try { await api.send(`/api/admin/devices/${id}/revoke`, "POST"); await load(); notify("裝置已撤銷"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function saveSettings(event: FormEvent) {
    event.preventDefault();
    if (!settings) return;
    try { setSettings(await api.send<NodeSettings>("/api/admin/settings", "PUT", settings)); notify("節點設定已更新"); } catch (reason) { notify(messageFor(reason)); }
  }
  return (
    <section className="page admin-page">
      <PageHeading eyebrow="NODE CONTROL" title="管理後台" description="集中管理帳號、容量、節點限制與稽核事件。" />
      {!totpReady && <form className="card create-user" onSubmit={setup ? enableTOTP : beginSetup}><h3>啟用雙因素驗證</h3><p className="muted">管理後台必須使用密碼與 TOTP 驗證。</p><label>目前密碼<input type="password" autoComplete="current-password" value={verification.password} onChange={(event) => setVerification({ ...verification, password: event.target.value })} required /></label>{setup && <><label>TOTP 密鑰<input readOnly value={setup.secret} /></label><small>請將密鑰加入驗證器後輸入六位數驗證碼。</small><label>驗證碼<input inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" value={verification.code} onChange={(event) => setVerification({ ...verification, code: event.target.value })} required /></label></>}<button className="primary">{setup ? "確認並啟用" : "產生 TOTP 密鑰"}</button></form>}
      {totpReady && !verified && <form className="card create-user" onSubmit={verify}><h3>重新驗證管理員</h3><p className="muted">驗證效力為 15 分鐘。</p><label>目前密碼<input type="password" autoComplete="current-password" value={verification.password} onChange={(event) => setVerification({ ...verification, password: event.target.value })} required /></label><label>六位數驗證碼<input inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" value={verification.code} onChange={(event) => setVerification({ ...verification, code: event.target.value })} required /></label><button className="primary">解鎖管理後台</button></form>}
      {verified && <>
      {adminLoadError && <div className="notice" role="alert"><span>{adminLoadError}</span><button onClick={() => void load()}>重試</button></div>}
      {adminLoading && !users.length && !adminDevices.length && !storage && <PanelLoader />}
      {nodeMetrics.at(-1) && <div className="metric-grid storage-metrics"><Metric label="CPU" value={`${nodeMetrics.at(-1)!.cpuPercent.toFixed(1)}%`} note="最新節點取樣" /><Metric label="記憶體" value={formatBytes(nodeMetrics.at(-1)!.memoryBytes)} note="系統使用量" /><Metric label="磁碟" value={formatBytes(nodeMetrics.at(-1)!.diskBytes)} note="檔案系統" /><Metric label="快取" value={formatBytes(nodeMetrics.at(-1)!.cacheBytes)} note="節點快取" /><Metric label="網路" value={formatBytes(nodeMetrics.at(-1)!.networkUploadBytes + nodeMetrics.at(-1)!.networkDownloadBytes)} note="上傳與下載" /><Metric label="在線／傳輸" value={`${nodeMetrics.at(-1)!.onlineDevices}／${nodeMetrics.at(-1)!.activeTransfers}`} note="即時工作" /></div>}
      <div className="card audit-list"><div className="list-title"><h3>傳輸失敗紀錄</h3><span>{failures.length} 筆</span></div>{failures.map((item) => <article key={`${item.transferId}:${item.targetDeviceId}`}><span className="audit-mark">!</span><p><strong>{item.errorCode || "TRANSFER_FAILED"}</strong><small>{item.transferId.slice(0, 8)} · {item.targetDeviceId.slice(0, 8)}</small></p><time>{formatDate(item.createdAt)}</time></article>)}{!failures.length && <Empty text="目前沒有傳輸失敗紀錄" />}</div>
      <div className="card settings-form"><div className="list-title"><div><p className="eyebrow">OPERATIONS</p><h3>節點維運</h3></div><span>主機管理指令</span></div><div className="settings-grid"><label>立即清理<code>deploy/nexdrop cleanup</code></label><label>建立備份<code>deploy/nexdrop backup --include-files</code></label><label>還原備份<code>deploy/nexdrop restore --file ... --confirm</code></label><label>安全更新<code>deploy/nexdrop update</code></label></div><small>備份、還原與更新須在節點主機執行，避免將 Docker 管理權限暴露給 Web 程序。</small></div>
      {settings && <div className="card settings-form"><h3>帳號建立政策</h3><label className="check"><input type="checkbox" checked={settings.publicRegistrationEnabled} onChange={(event) => setSettings({ ...settings, publicRegistrationEnabled: event.target.checked })} /> 公開註冊開關（第一版預設關閉）</label><small>變更後請在「節點與儲存」按下儲存設定；關閉時僅允許管理員建立或邀請帳號。</small></div>}
      <div className="admin-tabs"><button className={tab === "users" ? "active" : ""} onClick={() => setTab("users")}>使用者</button><button className={tab === "resources" ? "active" : ""} onClick={() => setTab("resources")}>裝置</button><button className={tab === "node" ? "active" : ""} onClick={() => setTab("node")}>節點與儲存</button><button className={tab === "audit" ? "active" : ""} onClick={() => setTab("audit")}>稽核與失敗（{failures.length}）</button></div>
      {tab === "users" && <div className="admin-layout"><form className="card create-user" onSubmit={createUser}><h3>{inviteMode ? "邀請使用者" : "建立使用者"}</h3><label className="check"><input type="checkbox" checked={inviteMode} onChange={(event) => { setInviteMode(event.target.checked); setInvitationLink(""); }} /> 由受邀者自行設定密碼</label><label>使用者名稱<input value={newUser.username} onChange={(event) => setNewUser({ ...newUser, username: event.target.value })} required /></label><label>電子郵件<input type="email" value={newUser.email} onChange={(event) => setNewUser({ ...newUser, email: event.target.value })} required /></label>{!inviteMode && <label>初始密碼<input type="password" value={newUser.password} onChange={(event) => setNewUser({ ...newUser, password: event.target.value })} minLength={12} required /></label>}<label className="check"><input type="checkbox" checked={newUser.admin} onChange={(event) => setNewUser({ ...newUser, admin: event.target.checked })} /> 管理員權限</label><button className="primary">{inviteMode ? "建立邀請連結" : "建立帳號"}</button>{invitationLink && <label>一次性邀請連結<input readOnly value={invitationLink} onFocus={(event) => event.currentTarget.select()} /><small>連結七天內有效，接受後立即失效。</small></label>}</form><div className="card user-list"><div className="list-title"><h3>所有使用者</h3><span>{users.length} 人</span></div>{users.map((item) => <article key={item.id}><span className="avatar small">{item.username[0]?.toUpperCase()}</span><p><strong>{item.username}</strong><small>{item.email}</small></p><Status value={item.disabledAt ? "DISABLED" : item.admin ? "ADMIN" : "ACTIVE"} />{!item.disabledAt && <button className="text-danger" onClick={() => disable(item.id)}>停用</button>}</article>)}</div></div>}
      {tab === "resources" && <div className="card user-list"><div className="list-title"><h3>所有裝置</h3><span>{adminDevices.length} 台</span></div>{adminDevices.map((item) => <article key={item.id}><span className="avatar small">{item.displayName[0]?.toUpperCase()}</span><p><strong>{item.displayName}</strong><small>{item.ownerUsername} · {item.type}</small></p><Status value={item.trustStatus} />{item.trustStatus !== "REVOKED" && <button className="text-danger" onClick={() => revokeDevice(item.id)}>撤銷</button>}</article>)}{!adminDevices.length && <Empty text="尚無裝置" />}</div>}
      {tab === "node" && <><div className="metric-grid storage-metrics"><Metric label="已存檔案" value={storage?.fileCount.toLocaleString() ?? "—"} note={formatBytes(storage?.storedBytes ?? 0)} /><Metric label="上傳中" value={formatBytes(storage?.uploadingBytes ?? 0)} note="暫存容量" /><Metric label="已過期" value={formatBytes(storage?.expiredBytes ?? 0)} note="等待清理" /><Metric label="配額使用" value={formatBytes(storage?.quotaBytesUsed ?? 0)} note={`上限 ${formatBytes(storage?.quotaByteLimit ?? 0)}`} /></div>{nodeMetrics.at(-1) && <div className="metric-grid storage-metrics"><Metric label="CPU" value={`${nodeMetrics.at(-1)!.cpuPercent.toFixed(1)}%`} note="最新節點取樣" /><Metric label="記憶體" value={formatBytes(nodeMetrics.at(-1)!.memoryBytes)} note="系統使用量" /><Metric label="在線設備" value={nodeMetrics.at(-1)!.onlineDevices.toLocaleString()} note="即時連線" /><Metric label="進行中傳輸" value={nodeMetrics.at(-1)!.activeTransfers.toLocaleString()} note="目前工作" /></div>}{settings && <form className="card settings-form" onSubmit={saveSettings}><div className="list-title"><div><p className="eyebrow">LIMITS</p><h3>節點限制</h3></div><button className="primary">儲存設定</button></div><div className="settings-grid">{settingFields.map((field) => <label key={field.key}>{field.label}<input type="number" min={1} value={settings[field.key]} onChange={(event) => setSettings({ ...settings, [field.key]: Number(event.target.value) })} /><small>{field.percent ? "百分比" : formatBytes(settings[field.key])}</small></label>)}</div></form>}</>}
      {tab === "audit" && <div className="card audit-list"><div className="list-title"><h3>最近事件</h3><span>{logs.length} 筆</span></div>{logs.map((item) => <article key={item.id}><span className="audit-mark">◆</span><p><strong>{item.action}</strong><small>{item.targetType}{item.targetId ? ` · ${item.targetId.slice(0, 8)}` : ""}</small></p><time>{formatDate(item.createdAt)}</time></article>)}{!logs.length && <Empty text="尚無稽核紀錄" />}</div>}
      </>}
    </section>
  );
}

async function loadAdminPages<T>(path: string) {
  const pageSize = 200;
  const result: T[] = [];
  for (let offset = 0; ; offset += pageSize) {
    const page = await api.get<T[]>(`${path}?limit=${pageSize}&offset=${offset}`);
    result.push(...page);
    if (page.length < pageSize) return result;
  }
}

const settingFields: Array<{ key: Exclude<keyof NodeSettings, "publicRegistrationEnabled">; label: string; percent?: boolean }> = [
  { key: "singleFileLimitBytes", label: "單檔上限" }, { key: "defaultUserQuotaBytes", label: "預設使用者配額" },
  { key: "nodeCacheLimitBytes", label: "節點快取上限" },
  { key: "defaultUserDailyBytes", label: "使用者每日流量" },
  { key: "diskWarningPercent", label: "磁碟警告門檻", percent: true }, { key: "diskStopPercent", label: "磁碟停止門檻", percent: true },
];

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
function labelDeviceType(value: string) { return ({ WINDOWS: "Windows", ANDROID: "Android", WEB_CHROME: "Chrome Web", WEB_EDGE: "Edge Web" } as Record<string, string>)[value] ?? value; }
function statusLabel(value: string) { return ({ ONLINE: "在線", OFFLINE: "離線", PENDING: "待核准", TRUSTED: "信任", REVOKED: "已撤銷", CREATED: "已建立", QUEUED: "佇列中", DELIVERED: "已送達", READ: "已讀", FAILED: "失敗", CANCELLED: "已取消", ACTIVE: "啟用", ADMIN: "管理員", DISABLED: "已停用" } as Record<string, string>)[value] ?? value.replaceAll("_", " "); }
function formatDate(value: string) { return new Intl.DateTimeFormat("zh-TW", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value)); }
function formatBytes(value: number) { if (!value) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }
function currentNodeStatisticsPath() { const now = Date.now(); return `/api/statistics/node?${new URLSearchParams({ from: new Date(now - 2 * 60 * 1000).toISOString(), to: new Date(now + 60 * 1000).toISOString() })}`; }
function fileMetadata(value: string | undefined, index: number) { try { return (JSON.parse(value ?? "") as Array<{ name: string; mimeType: string; size: number }>)[index]; } catch { return undefined; } }
function successRate(value: Overview) { const total = value.succeeded + value.failed; return total ? Math.round((value.succeeded / total) * 100) : 0; }
function messageFor(reason: unknown) { if (reason instanceof APIError) { const limited = rateLimitMessage(reason); if (limited) return limited; return ({ INVALID_REQUEST: "請確認所有必填欄位與格式", INVALID_CREDENTIALS: "帳號或密碼不正確", TOTP_REQUIRED: "請輸入驗證器中的六位數驗證碼", ADMIN_VERIFICATION_FAILED: "密碼或驗證碼不正確", INVALID_TOTP_SETUP: "無法啟用 TOTP，請確認密碼與驗證碼", ADMIN_REAUTH_REQUIRED: "管理員驗證已逾時，請重新驗證", PERMISSION_DENIED: "你沒有執行此操作的權限", INVALID_TOKEN: "登入已失效，請重新登入", ADMIN_RESOURCE_CONFLICT: "帳號或電子郵件已存在", INVALID_INVITATION: "邀請資料或密碼格式不正確", INVITATION_EXPIRED: "邀請連結無效、已使用或已逾期", INVITATION_ACCOUNT_CONFLICT: "此邀請的帳號或電子郵件已存在", INVALID_TRANSFER: "傳輸內容或目的地無效", QUOTA_EXCEEDED: "已超過可用配額", STORAGE_FULL: "節點儲存空間不足" } as Record<string, string>)[reason.code] ?? `操作失敗：${reason.code}`; } if (reason instanceof Error) return reason.message; return "操作失敗，請稍後再試"; }

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
