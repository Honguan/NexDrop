package api

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateWindow struct {
	count int
	reset time.Time
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]rateWindow
}

func newFixedWindowLimiter(limit int) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, windows: make(map[string]rateWindow)}
}

func (limiter *fixedWindowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	window := limiter.windows[key]
	if window.reset.IsZero() || !now.Before(window.reset) {
		window = rateWindow{reset: now.Add(time.Minute)}
	}
	window.count++
	limiter.windows[key] = window
	return window.count <= limiter.limit, window.reset.Sub(now)
}

func rateLimit(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func rateLimitKey(r *http.Request, identity string) string {
	host := r.RemoteAddr
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(identity))
}

func enforceRateLimit(
	w http.ResponseWriter,
	r *http.Request,
	limiter *fixedWindowLimiter,
	identity string,
) bool {
	allowed, retryAfter := limiter.allow(rateLimitKey(r, identity), time.Now().UTC())
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
	writeError(w, http.StatusTooManyRequests, "RATE_LIMITED")
	return false
}
