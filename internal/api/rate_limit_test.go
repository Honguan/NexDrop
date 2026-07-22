package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestFixedWindowLimiterResetsFromProvidedClock(t *testing.T) {
	limiter := newFixedWindowLimiter(2)
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

	for attempt := 1; attempt <= 3; attempt++ {
		allowed, retryAfter := limiter.allow("device", now)
		if allowed != (attempt <= 2) {
			t.Fatalf("attempt %d allowed = %v", attempt, allowed)
		}
		if retryAfter != time.Minute {
			t.Fatalf("attempt %d retryAfter = %s", attempt, retryAfter)
		}
	}

	allowed, retryAfter := limiter.allow("device", now.Add(time.Minute))
	if !allowed || retryAfter != time.Minute {
		t.Fatalf("reset allowed = %v, retryAfter = %s", allowed, retryAfter)
	}
}

func TestRateLimitKeyNormalizesAddressAndIdentity(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
	request.RemoteAddr = "[2001:db8::1]:443"

	if key := rateLimitKey(request, " Admin@Example.COM "); key != "2001:db8::1|admin@example.com" {
		t.Fatalf("rateLimitKey = %q", key)
	}
}
