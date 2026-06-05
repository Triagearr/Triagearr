package server

import (
	"container/list"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// authEnabledTTL caps how long the cached "is any auth_user registered" flag
// is trusted before re-checking the DB. The flag flips at most twice in the
// process lifetime (enable then disable), so the only constraint is that a
// fresh deployment should pick up the change within a few seconds.
const authEnabledTTL = 3 * time.Second

// touchInterval is the minimum gap between two sliding-window refreshes for
// the same session. Without this, every authenticated request pays a writer
// round-trip — and the writer pool serialises through a single connection.
const touchInterval = 5 * time.Minute

// handlerTimeout caps any single HTTP request. Long enough to cover scoring
// fetches under disk pressure (a few seconds) and worst-case *arr fan-outs;
// short enough that a stuck DB or hung upstream doesn't leak goroutines.
const handlerTimeout = 30 * time.Second

// withTimeout injects a per-request context deadline. It does not replace the
// HTTP server's ReadHeaderTimeout — that one guards the read side; this one
// caps the handler. Cancellation propagates to store + clients via r.Context().
func (s *Server) withTimeout(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
		defer cancel()
		h(w, r.WithContext(ctx))
	}
}

// security emits the default security headers on every response.
// CSP allows inline styles because Tailwind v4 emits some at runtime;
// `img-src data:` is needed for shadcn icons embedded as base64.
func (s *Server) security(h http.HandlerFunc) http.HandlerFunc {
	wrapped := s.withTimeout(h)
	return func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Permissions-Policy", "()")
		hd.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		wrapped(w, r)
	}
}

// auth gates a handler when built-in auth is enabled. It accepts either a
// valid session cookie or a matching X-API-Key header. When no user has
// been registered (built-in auth disabled) the handler is invoked directly.
// On success, an authenticated session is touched (sliding window) and the
// resolved user ID is attached to the request context.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled, err := s.authEnabled(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "auth state: "+err.Error())
			return
		}
		if !enabled {
			h(w, r)
			return
		}
		// 1. Cookie session.
		if uid, ok := s.resolveSession(r); ok {
			h(w, withUserID(r, uid))
			return
		}
		// 2. X-API-Key (programmatic clients).
		if s.opts.APIKey != "" {
			got := r.Header.Get("X-API-Key")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.APIKey)) == 1 {
				h(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, "authentication required")
	}
}

// authEnabled reports whether any user is registered in auth_users. The
// result is cached for authEnabledTTL so steady-state requests skip the DB.
func (s *Server) authEnabled(ctx context.Context) (bool, error) {
	if s.opts.Store == nil {
		return false, nil
	}
	if c, ok := s.authState.Load().(authStateCache); ok && time.Since(c.checkedAt) < authEnabledTTL {
		return c.enabled, nil
	}
	enabled, err := s.opts.Store.HasAuthUser(ctx)
	if err != nil {
		return false, err
	}
	s.authState.Store(authStateCache{enabled: enabled, checkedAt: time.Now()})
	return enabled, nil
}

// invalidateAuthState forces the next authEnabled call to re-query the DB.
// Called by handlers that toggle auth_users (enable / disable).
func (s *Server) invalidateAuthState() {
	s.authState.Store(authStateCache{})
}

// resolveSession looks up the session cookie; refreshes its sliding window
// only when stale enough to warrant a writer round-trip.
func (s *Server) resolveSession(r *http.Request) (int64, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return 0, false
	}
	hash := hashToken(c.Value)
	sess, err := s.opts.Store.LookupAuthSession(r.Context(), hash)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// Treat a DB error as anonymous; the next request will retry. Logged so
			// a failing auth DB is visible rather than silently degrading to anon.
			slog.Warn("auth: session lookup failed", "err", err)
		}
		return 0, false
	}
	if time.Since(sess.LastSeenAt) >= touchInterval {
		_ = s.opts.Store.TouchAuthSession(r.Context(), hash, sessionTTL)
	}
	return sess.UserID, true
}

// userIDKey type isolates the request-context key for the resolved user ID.
type userIDKey struct{}

func withUserID(r *http.Request, uid int64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userIDKey{}, uid))
}

// userIDFromContext returns the authenticated user ID and true if a cookie
// session resolved one. X-API-Key authentication doesn't bind to a user.
func userIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(userIDKey{}).(int64)
	return v, ok
}

// hashToken returns the hex-encoded sha256 of an opaque session token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ipRateLimiter caps the request rate per client IP. Used on destructive or
// sensitive endpoints so a misbehaving script or UI loop can't spam them.
//
// The map is bounded LRU-style: once `maxIPs` distinct IPs have been seen,
// the least recently used limiter is evicted on each insert. Without this,
// a scanner cycling through random IPs would grow the map indefinitely.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*list.Element
	lru      *list.List // front = most recent
	r        rate.Limit
	burst    int
	maxIPs   int
}

type ipLimiterEntry struct {
	ip  string
	lim *rate.Limiter
}

// ipLimiterCap caps the LRU at 10k distinct IPs — generous for a home-lab
// deployment while keeping the map bounded for any pathological caller.
const ipLimiterCap = 10_000

// newIPRateLimiter allows `burst` calls per `window` per IP.
func newIPRateLimiter(burst int, window time.Duration) *ipRateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &ipRateLimiter{
		limiters: make(map[string]*list.Element),
		lru:      list.New(),
		r:        rate.Every(window / time.Duration(burst)),
		burst:    burst,
		maxIPs:   ipLimiterCap,
	}
}

// buildRateLimiter resolves the configured per-minute cap to an ipRateLimiter.
// Convention: 0 applies the package default, negative disables (nil limiter
// = pass-through).
func buildRateLimiter(perMinute, defaultPerMinute int) *ipRateLimiter {
	if perMinute < 0 {
		return nil
	}
	burst := perMinute
	if burst == 0 {
		burst = defaultPerMinute
	}
	return newIPRateLimiter(burst, time.Minute)
}

func (l *ipRateLimiter) take(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.limiters[ip]; ok {
		l.lru.MoveToFront(el)
		return el.Value.(*ipLimiterEntry).lim.Allow()
	}
	if l.lru.Len() >= l.maxIPs {
		old := l.lru.Back()
		if old != nil {
			l.lru.Remove(old)
			delete(l.limiters, old.Value.(*ipLimiterEntry).ip)
		}
	}
	lim := rate.NewLimiter(l.r, l.burst)
	el := l.lru.PushFront(&ipLimiterEntry{ip: ip, lim: lim})
	l.limiters[ip] = el
	return lim.Allow()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimit returns a middleware that drops requests beyond lim's per-IP
// budget, responding with 429 and the given user-facing message. A nil
// limiter is a pass-through (rate limiting disabled via config).
func rateLimit(lim *ipRateLimiter, msg string) func(http.HandlerFunc) http.HandlerFunc {
	return func(h http.HandlerFunc) http.HandlerFunc {
		if lim == nil {
			return h
		}
		return func(w http.ResponseWriter, r *http.Request) {
			if !lim.take(clientIP(r)) {
				writeError(w, http.StatusTooManyRequests, msg)
				return
			}
			h(w, r)
		}
	}
}

func (s *Server) runRateLimit(h http.HandlerFunc) http.HandlerFunc {
	return rateLimit(s.runRate, "rate limit exceeded — try again in a minute")(h)
}

func (s *Server) authRateLimit(h http.HandlerFunc) http.HandlerFunc {
	return rateLimit(s.authRate, "too many auth attempts — try again later")(h)
}
