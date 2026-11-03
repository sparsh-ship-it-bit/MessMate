package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"os"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	count int
	reset time.Time
}

var requestRateLimits = struct {
	sync.Mutex
	entries map[string]rateLimitEntry
}{entries: make(map[string]rateLimitEntry)}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && isAllowedOrigin(origin, r) {
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}

	if strings.EqualFold(parsed.Host, r.Host) {
		requestScheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			requestScheme = "https"
		}
		if parsed.Scheme == requestScheme {
			return true
		}
	}

	allowed := strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",")
	if base := strings.TrimSpace(os.Getenv("APP_BASE_URL")); base != "" {
		allowed = append(allowed, base)
	}
	if !strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		allowed = append(allowed, "http://localhost:5173", "http://127.0.0.1:5173")
	}
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		u, parseErr := url.Parse(candidate)
		if parseErr == nil && u.Scheme != "" && u.Host != "" &&
			strings.EqualFold(u.Scheme, parsed.Scheme) && strings.EqualFold(u.Host, parsed.Host) &&
			u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
			return true
		}
	}
	return false
}

func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")

		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		if base, err := url.Parse(strings.TrimSpace(os.Getenv("APP_BASE_URL"))); err == nil && base.Scheme == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}

		limit, window := sensitiveRouteLimit(r.URL.Path)
		if limit > 0 {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			key := ip + "|" + r.URL.Path
			if !allowRateLimit(key, limit, window, time.Now()) {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(window.Seconds()))))
				errorJSON(w, http.StatusTooManyRequests, "too many requests; please try again later")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sensitiveRouteLimit(path string) (int, time.Duration) {
	switch path {
	case "/api/v1/auth/login":
		return 8, 5 * time.Minute
	case "/api/v1/auth/register":
		return 5, time.Hour
	case "/api/v1/auth/forgot-password":
		return 5, time.Hour
	case "/api/v1/mobile/pair":
		return 8, 5 * time.Minute
	case "/api/v1/mobile/pairing":
		return 5, 5 * time.Minute
	case "/api/v1/auth/reset-password":
		return 5, time.Hour
	default:
		return 0, 0
	}
}

func allowRateLimit(key string, limit int, window time.Duration, now time.Time) bool {
	requestRateLimits.Lock()
	defer requestRateLimits.Unlock()

	entry, ok := requestRateLimits.entries[key]
	if !ok || !now.Before(entry.reset) {
		requestRateLimits.entries[key] = rateLimitEntry{count: 1, reset: now.Add(window)}
		if len(requestRateLimits.entries) > 10000 {
			for k, v := range requestRateLimits.entries {
				if !now.Before(v.reset) {
					delete(requestRateLimits.entries, k)
				}
			}
		}
		return true
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	requestRateLimits.entries[key] = entry
	return true
}
