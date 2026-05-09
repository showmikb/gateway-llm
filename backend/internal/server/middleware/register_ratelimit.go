package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RegisterRateLimit is a small in-memory sliding-window limiter designed
// specifically for the public /register endpoint. It does NOT require Redis
// and keeps memory bounded by pruning stale entries on every call.
//
// Defaults (tunable via arguments): 5 requests per IP per hour.
type RegisterRateLimit struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	limit  int
}

func NewRegisterRateLimit(limit int, window time.Duration) *RegisterRateLimit {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Hour
	}
	return &RegisterRateLimit{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

func (rl *RegisterRateLimit) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.allow(ip) {
			writeError(w, http.StatusTooManyRequests, "too many registration attempts, please try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RegisterRateLimit) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	hits := rl.hits[ip]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= rl.limit {
		rl.hits[ip] = kept
		return false
	}

	kept = append(kept, now)
	rl.hits[ip] = kept

	// Opportunistic prune: if map grows large, drop empty/expired entries.
	if len(rl.hits) > 1024 {
		for k, v := range rl.hits {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(rl.hits, k)
			}
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.Index(fwd, ","); idx > 0 {
			return strings.TrimSpace(fwd[:idx])
		}
		return strings.TrimSpace(fwd)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
