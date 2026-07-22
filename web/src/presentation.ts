export type FileMetadata = {
  name: string;
  mimeType: string;
  size: number;
};

const deviceTypeLabels: Record<string, string> = {
  WINDOWS: "Windows",
  ANDROID: "Android",
  WEB_CHROME: "Chrome Web",
  WEB_EDGE: "Edge Web",
};

const statusLabels: Record<string, string> = {
  ONLINE: "在線",
  OFFLINE: "離線",
  PENDING: "待核准",
  TRUSTED: "信任",
  REVOKED: "已撤銷",
  CREATED: "已建立",
  QUEUED: "佇列中",
  DELIVERED: "已送達",
  READ: "已讀",
  FAILED: "失敗",
  CANCELLED: "已取消",
  ACTIVE: "啟用",
  ADMIN: "管理員",
  DISABLED: "已停用",
};

export function labelDeviceType(value: string) {
  return deviceTypeLabels[value] ?? value;
}

export function statusLabel(value: string) {
  return statusLabels[value] ?? value.replaceAll("_", " ");
}

export function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

export function fileMetadata(
  value: string | undefined,
  index: number,
): FileMetadata | undefined {
  if (!value || index < 0) return undefined;
  try {
    const parsed = JSON.parse(value) as unknown;
    if (!Array.isArray(parsed)) return undefined;
    const entry = parsed[index] as Partial<FileMetadata> | undefined;
    if (
      !entry ||
      typeof entry.name !== "string" ||
      typeof entry.mimeType !== "string" ||
      typeof entry.size !== "number"
    ) {
      return undefined;
    }
    return { name: entry.name, mimeType: entry.mimeType, size: entry.size };
  } catch {
    return undefined;
  }
}

export function successRate(value: { succeeded: number; failed: number }) {
  const total = value.succeeded + value.failed;
  return total > 0 ? Math.round((value.succeeded / total) * 100) : 0;
}
