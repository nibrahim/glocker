package stats

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIPRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	// No refill within the test window, so only the burst is available.
	l := newIPRateLimiter(rate.Every(time.Hour), 2, time.Hour)

	if !l.allow("1.1.1.1") {
		t.Fatal("first request should be allowed")
	}
	if !l.allow("1.1.1.1") {
		t.Fatal("second request (within burst) should be allowed")
	}
	if l.allow("1.1.1.1") {
		t.Fatal("third request should be blocked once the burst is spent")
	}
}

func TestIPRateLimiter_PerIPIndependent(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 1, time.Hour)

	if !l.allow("1.1.1.1") {
		t.Fatal("first IP should be allowed")
	}
	if l.allow("1.1.1.1") {
		t.Fatal("first IP should now be blocked")
	}
	if !l.allow("2.2.2.2") {
		t.Fatal("a different IP must have its own bucket")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{"xff single", "203.0.113.9", "127.0.0.1:5000", "203.0.113.9"},
		{"xff chain uses left-most", "203.0.113.9, 10.0.0.1", "127.0.0.1:5000", "203.0.113.9"},
		{"no xff falls back to peer", "", "198.51.100.7:44321", "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/register", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimited_Returns429AfterBurst(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 1, time.Hour)
	var served int
	h := rateLimited(l, func(w http.ResponseWriter, r *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	})

	call := func() int {
		r := httptest.NewRequest(http.MethodPost, "/api/register", nil)
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}

	if code := call(); code != http.StatusOK {
		t.Fatalf("first call: got %d, want 200", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("second call: got %d, want 429", code)
	}
	if served != 1 {
		t.Errorf("handler should have run once, ran %d times", served)
	}
}
