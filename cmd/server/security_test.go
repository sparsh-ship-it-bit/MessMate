package main

import (
	"net/http"
	"testing"
	"time"
)

func TestSensitiveRouteLimit(t *testing.T) {
	if n, _ := sensitiveRouteLimit("/api/v1/auth/login"); n != 8 {
		t.Fatalf("login limit = %d, want 8", n)
	}
	if n, _ := sensitiveRouteLimit("/api/v1/mobile/pair"); n != 8 {
		t.Fatalf("pairing limit = %d, want 8", n)
	}
	if n, _ := sensitiveRouteLimit("/api/v1/dashboard"); n != 0 {
		t.Fatalf("dashboard should not use the auth limiter, got %d", n)
	}
}

func TestAllowRateLimit(t *testing.T) {
	requestRateLimits.entries = make(map[string]rateLimitEntry)
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if !allowRateLimit("test", 2, time.Minute, now) {
			t.Fatalf("request %d unexpectedly blocked", i+1)
		}
	}
	if allowRateLimit("test", 2, time.Minute, now) {
		t.Fatal("third request should be blocked")
	}
	if !allowRateLimit("test", 2, time.Minute, now.Add(time.Minute)) {
		t.Fatal("request should be allowed after the window expires")
	}
}

func TestAllowedOriginRejectsUnlistedOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://messmate.example")
	t.Setenv("APP_BASE_URL", "")
	t.Setenv("APP_ENV", "production")
	r, _ := http.NewRequest(http.MethodGet, "https://api.example/health", nil)
	r.Host = "api.example"
	if isAllowedOrigin("https://evil.example", r) {
		t.Fatal("unlisted origin must not be allowed")
	}
	if !isAllowedOrigin("https://messmate.example", r) {
		t.Fatal("configured origin should be allowed")
	}
}
