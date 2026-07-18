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
