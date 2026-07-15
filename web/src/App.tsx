import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  AdminUser,
  APIError,
  AuditLog,
  Device,
  Group,
  NodeSettings,
  Overview,
  StorageOverview,
  Transfer,
  User,
  api,
  statisticsPath,
} from "./api";
import { decryptText, deviceID, encryptText, ensureDeviceKey, proveDeviceSession, rememberDevice } from "./crypto";

type View = "send" | "activity" | "devices" | "groups" | "analytics" | "admin";

const navItems: Array<{ id: View; label: string; glyph: string }> = [
  { id: "send", label: "傳送", glyph: "↗" },
  { id: "activity", label: "傳輸紀錄", glyph: "◷" },
  { id: "devices", label: "設備", glyph: "▣" },
  { id: "groups", label: "群組", glyph: "◎" },
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.login(identifier, password);
      onLogin(await api.get<User>("/api/account"));
    } catch (reason) {
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
          {error && <p className="form-error" role="alert">{error}</p>}
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
  const [groups, setGroups] = useState<Group[]>([]);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState("");
  const [online, setOnline] = useState(false);
  const [sharedContent, setSharedContent] = useState(readSharedContent);

  const reload = useCallback(async () => {
    const [nextDevices, nextGroups, nextTransfers] = await Promise.all([
      api.get<Device[]>("/api/devices"),
      api.get<Group[]>("/api/groups"),
      api.get<Transfer[]>("/api/transfers"),
    ]);
    setDevices(nextDevices);
    setGroups(nextGroups);
    setTransfers(nextTransfers);
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
        if (message.type === "connected") heartbeat = window.setInterval(() => socket?.send(JSON.stringify({ type: "heartbeat" })), 15000);
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
      case "send": return <SendView user={user} devices={devices} initialContent={sharedContent} onSent={async () => { await reload(); setSharedContent(""); }} notify={setNotice} />;
      case "activity": return <ActivityView user={user} devices={devices} transfers={transfers} />;
      case "devices": return <DevicesView user={user} devices={devices} reload={reload} notify={setNotice} />;
      case "groups": return <GroupsView groups={groups} reload={reload} notify={setNotice} />;
      case "analytics": return <AnalyticsView />;
      case "admin": return <AdminView notify={setNotice} />;
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

function SendView({ user, devices, initialContent, onSent, notify }: { user: User; devices: Device[]; initialContent: string; onSent: () => Promise<void>; notify: (value: string) => void }) {
  const [selected, setSelected] = useState<string[]>([]);
  const [content, setContent] = useState(initialContent);
  const [busy, setBusy] = useState(false);
  const trusted = devices.filter((item) => item.trustStatus === "TRUSTED" && item.publicKey);

  function toggle(id: string) {
    setSelected((current) => current.includes(id) ? current.filter((value) => value !== id) : [...current, id]);
  }

  async function send(event: FormEvent) {
    event.preventDefault();
    if (!content.trim() || selected.length === 0) return;
    setBusy(true);
    try {
      const recipients = trusted.filter((item) => selected.includes(item.id)).map((item) => ({ id: item.id, publicKey: item.publicKey! }));
      const encrypted = await encryptText(content.trim(), recipients);
      await api.send<Transfer>("/api/transfers", "POST", {
        targetType: selected.length === 1 ? "SINGLE_DEVICE" : "MULTIPLE_DEVICES",
        targetDeviceIds: selected,
        contentType: content.trim().startsWith("http") ? "URL" : "TEXT",
        routeMode: "AUTOMATIC",
        content: encrypted.content,
        wrappedContentKeys: encrypted.wrappedContentKeys,
      });
      setContent("");
      setSelected([]);
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
          <div className="card-title"><span className="step">01</span><div><h3>輸入內容</h3><p>文字與連結會在瀏覽器內先加密</p></div></div>
          <textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="貼上文字、網址或想傳給另一台設備的內容…" maxLength={100000} />
          <div className="composer-meta"><span>{content.length.toLocaleString()} 字元</span><span className="secure-pill">● 端對端加密</span></div>
          <div className="divider" />
          <div className="card-title"><span className="step">02</span><div><h3>選擇目的地</h3><p>{trusted.length ? `${trusted.length} 台信任設備可用` : "尚無信任設備"}</p></div></div>
          <div className="device-picker">
            {trusted.map((item) => (
              <button type="button" key={item.id} className={selected.includes(item.id) ? "device-option selected" : "device-option"} onClick={() => toggle(item.id)}>
                <DeviceGlyph type={item.type} /><span><strong>{item.displayName}</strong><small>{labelDeviceType(item.type)}</small></span><i>{selected.includes(item.id) ? "✓" : "+"}</i>
              </button>
            ))}
            {!trusted.length && <Empty text={user.admin ? "前往「設備」建立並核准這個瀏覽器" : "請由管理員核准這個瀏覽器設備"} />}
          </div>
          <button className="primary send-button" disabled={busy || !content.trim() || selected.length === 0}>{busy ? "正在加密…" : <>建立安全傳輸 <span>↗</span></>}</button>
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

function ActivityView({ user, devices, transfers }: { user: User; devices: Device[]; transfers: Transfer[] }) {
  const [decrypted, setDecrypted] = useState<Record<string, string>>({});
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

  return (
    <section className="page">
      <PageHeading eyebrow="ACTIVITY" title="傳輸紀錄" description="最近建立與接收的任務、路徑與交付狀態。" />
      <div className="table-card card">
        <div className="table-head"><span>內容</span><span>目的地</span><span>路徑</span><span>狀態</span><span>時間</span></div>
        {transfers.map((item) => (
          <article className="table-row" key={item.id}>
            <div><span className="content-glyph">{item.contentType === "URL" ? "↗" : "T"}</span><p><strong>{decrypted[item.id] ?? (item.contentType === "TEXT" ? "加密文字" : item.contentType)}</strong><small>{item.id.slice(0, 8)}</small></p></div>
            <span>{item.targets.map((target) => names[target.deviceId] ?? target.deviceId.slice(0, 8)).join("、")}</span>
            <span className="route-label">{item.targets[0]?.selectedRoute ?? "—"}</span>
            <Status value={item.status} />
            <time>{formatDate(item.createdAt)}</time>
          </article>
        ))}
        {!transfers.length && <Empty text="還沒有傳輸紀錄" />}
      </div>
    </section>
  );
}

function DevicesView({ user, devices, reload, notify }: { user: User; devices: Device[]; reload: () => Promise<void>; notify: (value: string) => void }) {
  const [busy, setBusy] = useState(false);
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
      notify("已建立此瀏覽器設備，等待信任核准");
    } catch (reason) { notify(messageFor(reason)); } finally { setBusy(false); }
  }
  async function approve(id: string) {
    try { await api.send(`/api/devices/${id}/approve`, "POST"); await reload(); notify("設備已核准"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function revoke(id: string) {
    try { await api.send(`/api/devices/${id}/revoke`, "POST"); await reload(); notify("設備已撤銷"); } catch (reason) { notify(messageFor(reason)); }
  }
  return (
    <section className="page">
      <PageHeading eyebrow="TRUSTED DEVICES" title="設備" description="只有核准且持有私鑰的設備能解開傳輸內容。" action={<button className="primary" onClick={register} disabled={busy || registered}>{busy ? "建立中…" : registered ? "此瀏覽器已登記" : "+ 登記此瀏覽器"}</button>} />
      <div className="cards-grid devices-grid">
        {devices.map((item) => <article className="device-card card" key={item.id}><div className="device-top"><DeviceGlyph type={item.type} /><Status value={item.trustStatus} /></div><h3>{item.displayName}</h3><p>{labelDeviceType(item.type)} · {formatDate(item.createdAt)}</p><div className="device-actions">{user.admin && item.trustStatus === "PENDING" && <button className="secondary" onClick={() => approve(item.id)}>核准</button>}{item.trustStatus !== "REVOKED" && <button className="text-danger" onClick={() => revoke(item.id)}>撤銷</button>}</div></article>)}
        {!devices.length && <Empty text="尚未登記任何設備" />}
      </div>
    </section>
  );
}

function GroupsView({ groups, reload, notify }: { groups: Group[]; reload: () => Promise<void>; notify: (value: string) => void }) {
  const [name, setName] = useState("");
  async function create(event: FormEvent) {
    event.preventDefault();
    try { await api.send("/api/groups", "POST", { name }); setName(""); await reload(); notify("群組已建立"); } catch (reason) { notify(messageFor(reason)); }
  }
  return (
    <section className="page">
      <PageHeading eyebrow="SHARED SPACES" title="群組" description="將成員與設備組成固定的傳輸目的地。" />
      <form className="inline-create card" onSubmit={create}><label><span>新群組名稱</span><input value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：設計團隊" required maxLength={100} /></label><button className="primary">建立群組</button></form>
      <div className="cards-grid group-grid">{groups.map((item) => <article className="group-card card" key={item.id}><span className="group-mark">◎</span><div><h3>{item.name}</h3><p>{item.role === "OWNER" ? "你是擁有者" : item.role}</p></div><time>{formatDate(item.createdAt)}</time></article>)}{!groups.length && <Empty text="建立第一個群組，讓固定協作更快" />}</div>
    </section>
  );
}

function AnalyticsView() {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { api.get<Overview>(statisticsPath("/api/statistics/overview")).then(setOverview).catch((reason) => setError(messageFor(reason))); }, []);
  return (
    <section className="page">
      <PageHeading eyebrow="7 DAY OVERVIEW" title="傳輸統計" description="掌握流量、成功率與實際使用的傳輸路徑。" />
      {error ? <Empty text={error} /> : !overview ? <PanelLoader /> : <><div className="metric-grid"><Metric label="傳輸任務" value={overview.transferCount.toLocaleString()} note="最近 7 天" /><Metric label="傳輸容量" value={formatBytes(overview.totalBytes)} note="全部路徑" /><Metric label="成功交付" value={overview.succeeded.toLocaleString()} note={`${successRate(overview)}% 成功率`} /><Metric label="失敗" value={overview.failed.toLocaleString()} note="可於紀錄中追蹤" danger={overview.failed > 0} /></div><div className="card route-summary"><div><p className="eyebrow">ROUTE MIX</p><h3>傳輸路徑分布</h3></div>{Object.entries(overview.routeCounts ?? {}).map(([route, count]) => <div className="route-bar" key={route}><span>{route}</span><i><b style={{ width: `${Math.max(4, (count / Math.max(overview.transferCount, 1)) * 100)}%` }} /></i><strong>{count}</strong></div>)}{!Object.keys(overview.routeCounts ?? {}).length && <Empty text="尚無足夠資料" />}</div></>}
    </section>
  );
}

function AdminView({ notify }: { notify: (value: string) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [storage, setStorage] = useState<StorageOverview | null>(null);
  const [settings, setSettings] = useState<NodeSettings | null>(null);
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [tab, setTab] = useState<"users" | "node" | "audit">("users");
  const [newUser, setNewUser] = useState({ username: "", email: "", password: "", admin: false });

  const load = useCallback(async () => {
    const [nextUsers, nextStorage, nextSettings, nextLogs] = await Promise.all([
      api.get<AdminUser[]>("/api/admin/users"), api.get<StorageOverview>("/api/admin/storage"),
      api.get<NodeSettings>("/api/admin/settings"), api.get<AuditLog[]>("/api/admin/audit-logs"),
    ]);
    setUsers(nextUsers); setStorage(nextStorage); setSettings(nextSettings); setLogs(nextLogs);
  }, []);
  useEffect(() => { load().catch((reason) => notify(messageFor(reason))); }, [load, notify]);
  async function createUser(event: FormEvent) {
    event.preventDefault();
    try { await api.send("/api/admin/users", "POST", newUser); setNewUser({ username: "", email: "", password: "", admin: false }); await load(); notify("使用者已建立"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function disable(id: string) {
    try { await api.send(`/api/admin/users/${id}`, "DELETE"); await load(); notify("使用者已停用"); } catch (reason) { notify(messageFor(reason)); }
  }
  async function saveSettings(event: FormEvent) {
    event.preventDefault();
    if (!settings) return;
    try { setSettings(await api.send<NodeSettings>("/api/admin/settings", "PUT", settings)); notify("節點設定已更新"); } catch (reason) { notify(messageFor(reason)); }
  }
  return (
    <section className="page admin-page">
      <PageHeading eyebrow="NODE CONTROL" title="管理後台" description="集中管理帳號、容量、節點限制與稽核事件。" />
      <div className="admin-tabs"><button className={tab === "users" ? "active" : ""} onClick={() => setTab("users")}>使用者</button><button className={tab === "node" ? "active" : ""} onClick={() => setTab("node")}>節點與儲存</button><button className={tab === "audit" ? "active" : ""} onClick={() => setTab("audit")}>稽核紀錄</button></div>
      {tab === "users" && <div className="admin-layout"><form className="card create-user" onSubmit={createUser}><h3>建立使用者</h3><label>使用者名稱<input value={newUser.username} onChange={(event) => setNewUser({ ...newUser, username: event.target.value })} required /></label><label>電子郵件<input type="email" value={newUser.email} onChange={(event) => setNewUser({ ...newUser, email: event.target.value })} required /></label><label>初始密碼<input type="password" value={newUser.password} onChange={(event) => setNewUser({ ...newUser, password: event.target.value })} minLength={12} required /></label><label className="check"><input type="checkbox" checked={newUser.admin} onChange={(event) => setNewUser({ ...newUser, admin: event.target.checked })} /> 管理員權限</label><button className="primary">建立帳號</button></form><div className="card user-list"><div className="list-title"><h3>所有使用者</h3><span>{users.length} 人</span></div>{users.map((item) => <article key={item.id}><span className="avatar small">{item.username[0]?.toUpperCase()}</span><p><strong>{item.username}</strong><small>{item.email}</small></p><Status value={item.disabledAt ? "DISABLED" : item.admin ? "ADMIN" : "ACTIVE"} />{!item.disabledAt && <button className="text-danger" onClick={() => disable(item.id)}>停用</button>}</article>)}</div></div>}
      {tab === "node" && <><div className="metric-grid storage-metrics"><Metric label="已存檔案" value={storage?.fileCount.toLocaleString() ?? "—"} note={formatBytes(storage?.storedBytes ?? 0)} /><Metric label="上傳中" value={formatBytes(storage?.uploadingBytes ?? 0)} note="暫存容量" /><Metric label="已過期" value={formatBytes(storage?.expiredBytes ?? 0)} note="等待清理" /><Metric label="配額使用" value={formatBytes(storage?.quotaBytesUsed ?? 0)} note={`上限 ${formatBytes(storage?.quotaByteLimit ?? 0)}`} /></div>{settings && <form className="card settings-form" onSubmit={saveSettings}><div className="list-title"><div><p className="eyebrow">LIMITS</p><h3>節點限制</h3></div><button className="primary">儲存設定</button></div><div className="settings-grid">{settingFields.map((field) => <label key={field.key}>{field.label}<input type="number" min={1} value={settings[field.key]} onChange={(event) => setSettings({ ...settings, [field.key]: Number(event.target.value) })} /><small>{field.percent ? "百分比" : formatBytes(settings[field.key])}</small></label>)}</div></form>}</>}
      {tab === "audit" && <div className="card audit-list"><div className="list-title"><h3>最近事件</h3><span>{logs.length} 筆</span></div>{logs.map((item) => <article key={item.id}><span className="audit-mark">◆</span><p><strong>{item.action}</strong><small>{item.targetType}{item.targetId ? ` · ${item.targetId.slice(0, 8)}` : ""}</small></p><time>{formatDate(item.createdAt)}</time></article>)}{!logs.length && <Empty text="尚無稽核紀錄" />}</div>}
    </section>
  );
}

const settingFields: Array<{ key: keyof NodeSettings; label: string; percent?: boolean }> = [
  { key: "singleFileLimitBytes", label: "單檔上限" }, { key: "defaultUserQuotaBytes", label: "預設使用者配額" },
  { key: "defaultGroupQuotaBytes", label: "預設群組配額" }, { key: "nodeCacheLimitBytes", label: "節點快取上限" },
  { key: "defaultUserDailyBytes", label: "使用者每日流量" }, { key: "defaultGroupDailyBytes", label: "群組每日流量" },
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
function statusLabel(value: string) { return ({ PENDING: "待核准", TRUSTED: "信任", REVOKED: "已撤銷", CREATED: "已建立", QUEUED: "佇列中", DELIVERED: "已送達", READ: "已讀", FAILED: "失敗", CANCELLED: "已取消", ACTIVE: "啟用", ADMIN: "管理員", DISABLED: "已停用" } as Record<string, string>)[value] ?? value.replaceAll("_", " "); }
function formatDate(value: string) { return new Intl.DateTimeFormat("zh-TW", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value)); }
function formatBytes(value: number) { if (!value) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }
function successRate(value: Overview) { const total = value.succeeded + value.failed; return total ? Math.round((value.succeeded / total) * 100) : 0; }
function messageFor(reason: unknown) { if (reason instanceof APIError) return ({ INVALID_CREDENTIALS: "帳號或密碼不正確", PERMISSION_DENIED: "你沒有執行此操作的權限", INVALID_TOKEN: "登入已失效，請重新登入", ADMIN_RESOURCE_CONFLICT: "帳號或電子郵件已存在", INVALID_TRANSFER: "傳輸內容或目的地無效", QUOTA_EXCEEDED: "已超過可用配額", STORAGE_FULL: "節點儲存空間不足" } as Record<string, string>)[reason.code] ?? `操作失敗：${reason.code}`; if (reason instanceof Error) return reason.message; return "操作失敗，請稍後再試"; }

function readSharedContent() {
  if (!location.hash.startsWith("#share=")) return "";
  try {
    const encoded = location.hash.slice(7).replaceAll("-", "+").replaceAll("_", "/");
    const padded = encoded.padEnd(Math.ceil(encoded.length / 4) * 4, "=");
    const binary = atob(padded);
    const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    const payload = JSON.parse(new TextDecoder().decode(bytes)) as { kind?: string; title?: string; url?: string; text?: string };
    history.replaceState(null, "", `${location.pathname}${location.search}`);
    if (payload.kind === "SELECTION") return [payload.text, payload.url].filter(Boolean).join("\n\n");
    return payload.url ?? payload.text ?? "";
  } catch {
    history.replaceState(null, "", `${location.pathname}${location.search}`);
    return "";
  }
}
