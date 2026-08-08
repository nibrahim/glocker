package stats

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipRateLimiter throttles requests per client IP with a token bucket. It guards
// the open, unauthenticated registration endpoint against abuse — bulk signups
// that would flood real inboxes with verification mail and burn the mail quota.
// Idle buckets are swept so the map stays bounded.
type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	limit   rate.Limit
	burst   int
	ttl     time.Duration
}

type ipBucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// newIPRateLimiter builds a limiter granting burst tokens up front and refilling
// at limit tokens/second. ttl bounds how long an idle IP's bucket is retained.
func newIPRateLimiter(limit rate.Limit, burst int, ttl time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		buckets: make(map[string]*ipBucket),
		limit:   limit,
		burst:   burst,
		ttl:     ttl,
	}
}

// allow consumes a token for ip, reporting whether the request may proceed.
func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &ipBucket{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[ip] = b
	}
	b.seen = now

	// Opportunistically drop buckets that have gone idle, so a stream of unique
	// IPs can't grow the map without bound.
	if len(l.buckets) > 1024 {
		for k, v := range l.buckets {
			if now.Sub(v.seen) > l.ttl {
				delete(l.buckets, k)
			}
		}
	}
	return b.lim.Allow()
}

// clientIP is the best-effort client address used as the rate-limit key. In
// hosted mode glockpeek sits behind a trusted reverse proxy (Caddy) that sets
// X-Forwarded-For; its left-most entry is the original client. Fall back to the
// transport peer when there's no proxy header.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// rateLimited wraps h, rejecting a client that exceeds l with 429 plus a
// Retry-After hint rather than running the handler.
func rateLimited(l *ipRateLimiter, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests; please wait and try again", http.StatusTooManyRequests)
			return
		}
		h(w, r)
	}
}
