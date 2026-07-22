export type RateLimitError = {
  code: string;
  retryAfterSeconds?: number;
};

export function retryAfterSeconds(value: string | null) {
  if (!value) return undefined;
  const seconds = Number.parseInt(value, 10);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}

export function rateLimitMessage(error: RateLimitError) {
  if (error.code !== "RATE_LIMITED") return null;
  return error.retryAfterSeconds
    ? `操作過於頻繁，請在 ${error.retryAfterSeconds} 秒後再試。`
    : "操作過於頻繁，請稍後再試。";
}

const apiMessages: Record<string, string> = {
  INVALID_REQUEST: "請確認所有必填欄位與格式",
  INVALID_CREDENTIALS: "帳號或密碼不正確",
  TOTP_REQUIRED: "請輸入驗證器中的六位數驗證碼",
  PERMISSION_DENIED: "你沒有執行此操作的權限",
  INVALID_TOKEN: "登入已失效，請重新登入",
  NODE_KEY_REQUIRED: "節點密鑰不正確或尚未設定",
  INVALID_TRANSFER: "傳輸內容或目的地無效",
  QUOTA_EXCEEDED: "已超過可用配額",
  STORAGE_FULL: "節點儲存空間不足",
};

export function messageFor(reason: unknown) {
  if (isAPIError(reason)) {
    const limited = rateLimitMessage(reason);
    if (limited) return limited;
    return apiMessages[reason.code] ?? `操作失敗：${reason.code}`;
  }
  if (reason instanceof Error) return reason.message;
  return "操作失敗，請稍後再試";
}

function isAPIError(reason: unknown): reason is Error & RateLimitError & { status: number } {
  if (!(reason instanceof Error)) return false;
  const candidate = reason as Error & Partial<RateLimitError> & { status?: unknown };
  return typeof candidate.code === "string" && typeof candidate.status === "number";
}
