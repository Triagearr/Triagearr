package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
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
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Permissions-Policy", "()")
		hd.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		h(w, r)
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

// authEnabled reports whether any user is registered in auth_users.
func (s *Server) authEnabled(ctx context.Context) (bool, error) {
	if s.opts.Store == nil {
		return false, nil
	}
	return s.opts.Store.HasAuthUser(ctx)
}

// resolveSession looks up the session cookie; touches it on success.
func (s *Server) resolveSession(r *http.Request) (int64, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return 0, false
	}
	hash := hashToken(c.Value)
	sess, err := s.opts.Store.LookupAuthSession(r.Context(), hash)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// log via slog at the call site if needed; for an auth lookup
			// failure we simply treat as anonymous.
			_ = err
		}
		return 0, false
	}
	// Sliding refresh; best-effort.
	_ = s.opts.Store.TouchAuthSession(r.Context(), hash, sessionTTL)
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
		r:        rate.Every(window / time.Duration(burst)),
		burst:    burst,
		window:   window,
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
	if s.runRate == nil {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.runRate.take(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded — try again in a minute")
			return
		}
		h(w, r)
	}
}

func (s *Server) authRateLimit(h http.HandlerFunc) http.HandlerFunc {
	if s.authRate == nil {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authRate.take(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "too many auth attempts — try again later")
			return
		}
		h(w, r)
	}
}
