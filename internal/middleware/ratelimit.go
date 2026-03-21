package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a simple per-user sliding window rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	windows  map[string]*window
	limit    int
	interval time.Duration
}

type window struct {
	count    int
	resetAt  time.Time
}

// NewRateLimiter creates a rate limiter allowing limit requests per interval per user.
func NewRateLimiter(limit int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		windows:  make(map[string]*window),
		limit:    limit,
		interval: interval,
	}
}

// Limit returns middleware that enforces the rate limit using the user ID
// from the auth context, falling back to the remote IP for unauthenticated
// requests.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := UserIDFromContext(r.Context())
		if key == "" {
			key = r.RemoteAddr
		}

		if !rl.allow(key) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please slow down")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, exists := rl.windows[key]
	if !exists || now.After(w.resetAt) {
		rl.windows[key] = &window{
			count:   1,
			resetAt: now.Add(rl.interval),
		}
		return true
	}

	if w.count >= rl.limit {
		return false
	}

	w.count++
	return true
}
