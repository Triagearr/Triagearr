package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// security emits the default security headers on every response.
// CSP allows inline styles because Tailwind v4 emits some at runtime;
// `img-src data:` is needed for shadcn icons embedded as base64.
func (s *Server) security(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h2 := w.Header()
		h2.Set("X-Content-Type-Options", "nosniff")
		h2.Set("Referrer-Policy", "no-referrer")
		h2.Set("Permissions-Policy", "()")
		h2.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		h(w, r)
	}
}

// ipRateLimiter caps the request rate per client IP. Used on the destructive
// POST /runs endpoint so a misbehaving script or UI loop can't spam decisions.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	burst    int
	window   time.Duration
}

// newIPRateLimiter allows `burst` calls per `window` per IP.
func newIPRateLimiter(burst int, window time.Duration) *ipRateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        rate.Every(window),
		burst:    burst,
		window:   window,
	}
}

func (l *ipRateLimiter) take(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.limiters[ip]
	if !ok {
		lim = rate.NewLimiter(l.r, l.burst)
		l.limiters[ip] = lim
	}
	return lim.Allow()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) runRateLimit(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !s.runRate.take(ip) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded — try again in a minute")
			return
		}
		h(w, r)
	}
}
