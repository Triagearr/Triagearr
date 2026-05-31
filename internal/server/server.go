// Package server exposes Triagearr's HTTP API and serves the embedded React UI.
//
// Authentication is opt-in via the dashboard (ADR-0019): when no user is
// registered in auth_users the API is open and the operator relies on
// whatever upstream protection they configure (TinyAuth, Authelia, private
// network, nothing). Once enabled from Settings → Security, every
// /api/v1/* request requires either a valid session cookie or a matching
// X-API-Key header. The two paths coexist so programmatic clients keep
// working while operators get cookie-based UX.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/store"
)

// VersionInfo is the build metadata surfaced through GET /api/v1/version.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Engine bundles the config-derived subsystems the HTTP handlers read. It is
// immutable once built: on reload the daemon constructs a fresh Engine and
// swaps it in atomically via Server.SwapEngine (Option B hot reload), so a
// request in flight keeps a consistent snapshot rather than reading a mix of
// old and new fields mid-handler.
type Engine struct {
	Config *config.Config

	// Scorer drives the single-hash rescore on protect-toggle so the Decider's
	// view (excluded yes/no) updates without waiting for the next pass. Nil in
	// tests that don't exercise the protect endpoint.
	Scorer *scorer.Scorer

	Decider *decider.Decider
	// Volume returns the single watched volume the Decider plans against
	// (ADR-0024). The daemon supplies a closure over the parsed config.
	Volume func() decider.Volume
	// DaemonLive mirrors config.Mode == "live". Without it, per-request live
	// opt-ins are forced back to dry-run (ADR-0015).
	DaemonLive bool
	Actor      *actor.Actor

	// Notifier is the configured notification dispatcher. It backs the
	// "send test notification" endpoint. Nil/empty when no provider is set.
	Notifier *notify.Dispatcher
}

// Options bundles the long-lived dependencies the server needs at construction
// time — those that survive a config reload (store, listener, build metadata,
// reload hooks). Config-derived subsystems live in Engine instead.
type Options struct {
	Bind      string
	APIKey    string
	Store     *store.Store
	Linker    *linker.Linker
	Version   VersionInfo
	UIHandler http.Handler

	// RunsPerMinute and AuthPerMinute control the per-IP rate limits. 0
	// applies the package default (60 / 30); negative disables. See
	// config.RateLimitsConfig for the source of these values.
	RunsPerMinute int
	AuthPerMinute int

	// ConfigPath is the YAML config file path. When set, the settings handler
	// loads it without overrides to compute baseline (pre-override) values,
	// which the UI shows on hover over an overridden field.
	ConfigPath string

	// Reload, when non-nil, asks the daemon to rebuild the Engine from the
	// freshly persisted settings_overrides and swap it in. It blocks until the
	// swap is live (or ctx is cancelled) and returns the build/validation
	// error, so the settings handler can report failure synchronously instead
	// of the old fire-and-forget self-SIGHUP. Nil in tests.
	Reload func(context.Context) error

	// ReloadValidate dry-runs a candidate override set through the full
	// config load pipeline (YAML + overrides + Validate) so PUT can reject
	// invalid combinations before persisting anything. Required for the
	// settings endpoints — they return 503 when nil.
	ReloadValidate func(overrides []config.Override) error
}

// sessionTTL is the sliding window applied on every authenticated hit.
const sessionTTL = 7 * 24 * time.Hour

// Server is a wired HTTP server ready to be Started.
type Server struct {
	opts     Options
	srv      *http.Server
	runRate  *ipRateLimiter
	authRate *ipRateLimiter

	// eng holds the current config-derived Engine. Read via engine() on every
	// handler hit and replaced wholesale by SwapEngine on reload — the listener
	// itself never restarts (Option B).
	eng atomic.Pointer[Engine]

	// baseCtx is the daemon-lifetime context, captured in Start. Async live
	// runs detach onto it so a deletion pipeline outlives the HTTP request
	// that triggered it (and is cancelled on daemon shutdown). Background
	// (no cancellation) until Start runs — fine for tests that never Start.
	baseCtx context.Context
	// liveRun is a capacity-1 semaphore: at most one live run executes at a
	// time. A second concurrent live trigger is rejected with 409 rather than
	// racing the destructive pipeline against itself.
	liveRun chan struct{}

	// authState caches the "is any user registered" flag and the timestamp
	// of the last DB check. Used by middleware.auth on every request to
	// avoid a per-request SELECT COUNT(*).
	authState atomic.Value // holds authStateCache
}

// authStateCache is the atomic snapshot stored on Server.authState.
type authStateCache struct {
	enabled   bool
	checkedAt time.Time
}

// New builds a Server with its initial Engine. Does not start listening. The
// Engine may be swapped later via SwapEngine without restarting the listener.
func New(opts Options, eng *Engine) *Server {
	s := &Server{
		opts:     opts,
		runRate:  buildRateLimiter(opts.RunsPerMinute, 60),
		authRate: buildRateLimiter(opts.AuthPerMinute, 30),
		baseCtx:  context.Background(),
		liveRun:  make(chan struct{}, 1),
	}
	s.eng.Store(eng)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/runs", s.security(s.auth(s.runRateLimit(s.handlePostRun))))
	mux.HandleFunc("GET /api/v1/runs/preview", s.security(s.auth(s.handlePreviewRun)))
	mux.HandleFunc("GET /api/v1/runs", s.security(s.auth(s.handleListRuns)))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.security(s.auth(s.handleGetRun)))
	mux.HandleFunc("GET /api/v1/runs/{id}/actions", s.security(s.auth(s.handleRunActions)))
	mux.HandleFunc("GET /api/v1/actions", s.security(s.auth(s.handleListActions)))
	mux.HandleFunc("GET /api/v1/actions/{id}", s.security(s.auth(s.handleGetAction)))
	mux.HandleFunc("GET /api/v1/torrents", s.security(s.auth(s.handleListTorrents)))
	mux.HandleFunc("GET /api/v1/torrents/categories", s.security(s.auth(s.handleTorrentCategories)))
	mux.HandleFunc("GET /api/v1/torrents/{hash}", s.security(s.auth(s.handleGetTorrent)))
	mux.HandleFunc("GET /api/v1/torrents/{hash}/snapshots", s.security(s.auth(s.handleTorrentSnapshots)))
	mux.HandleFunc("PUT /api/v1/torrents/{hash}/protected", s.security(s.auth(s.handleSetTorrentProtected)))
	mux.HandleFunc("PUT /api/v1/torrents/{hash}/candidate_boost", s.security(s.auth(s.handleSetTorrentCandidateBoost)))
	mux.HandleFunc("GET /api/v1/scores", s.security(s.auth(s.handleListScores)))
	mux.HandleFunc("GET /api/v1/volume", s.security(s.auth(s.handleVolume)))
	mux.HandleFunc("GET /api/v1/volume/history", s.security(s.auth(s.handleVolumeHistory)))
	mux.HandleFunc("GET /api/v1/arrs", s.security(s.auth(s.handleListArrs)))
	mux.HandleFunc("GET /api/v1/summary", s.security(s.auth(s.handleSummary)))
	mux.HandleFunc("GET /api/v1/config", s.security(s.auth(s.handleConfig)))
	mux.HandleFunc("GET /api/v1/version", s.security(s.auth(s.handleVersion)))
	mux.HandleFunc("GET /api/v1/settings", s.security(s.auth(s.handleGetSettings)))
	mux.HandleFunc("PUT /api/v1/settings", s.security(s.auth(s.handlePutSettings)))
	mux.HandleFunc("DELETE /api/v1/settings/{key}", s.security(s.auth(s.handleDeleteSetting)))
	mux.HandleFunc("POST /api/v1/notifications/test", s.security(s.auth(s.handleTestNotification)))
	mux.HandleFunc("GET /api/v1/scoring/defaults", s.security(s.auth(s.handleGetScoringDefaults)))
	mux.HandleFunc("PUT /api/v1/scoring/defaults", s.security(s.auth(s.handlePutScoringDefaults)))
	mux.HandleFunc("POST /api/v1/scoring/simulate", s.security(s.auth(s.handleSimulateScoring)))
	mux.HandleFunc("GET /api/v1/scoring/tracker-policies", s.security(s.auth(s.handleListTrackerPolicies)))
	mux.HandleFunc("PUT /api/v1/scoring/tracker-policies/{host}", s.security(s.auth(s.handlePutTrackerPolicy)))
	mux.HandleFunc("DELETE /api/v1/scoring/tracker-policies/{host}", s.security(s.auth(s.handleDeleteTrackerPolicy)))
	mux.HandleFunc("GET /api/v1/arr-connections", s.security(s.auth(s.handleListArrConnections)))
	mux.HandleFunc("POST /api/v1/arr-connections/test", s.security(s.auth(s.handleTestArrConnection)))
	mux.HandleFunc("PUT /api/v1/arr-connections/{kind}", s.security(s.auth(s.handleUpsertArrConnection)))
	mux.HandleFunc("DELETE /api/v1/arr-connections/{kind}", s.security(s.auth(s.handleDeleteArrConnection)))
	mux.HandleFunc("GET /api/v1/torrent-client-connections", s.security(s.auth(s.handleListTorrentClientConnections)))
	mux.HandleFunc("POST /api/v1/torrent-client-connections/test", s.security(s.auth(s.handleTestTorrentClientConnection)))
	mux.HandleFunc("PUT /api/v1/torrent-client-connections/{kind}", s.security(s.auth(s.handleUpsertTorrentClientConnection)))
	mux.HandleFunc("DELETE /api/v1/torrent-client-connections/{kind}", s.security(s.auth(s.handleDeleteTorrentClientConnection)))

	// Auth endpoints. GET /session is unauthenticated (the SPA uses it to
	// decide whether to show the login screen). POST /session and the
	// /auth/* mutators run under a stricter per-IP rate limit.
	mux.HandleFunc("GET /api/v1/session", s.security(s.handleSessionStatus))
	mux.HandleFunc("POST /api/v1/session", s.security(s.authRateLimit(s.handleSessionLogin)))
	mux.HandleFunc("DELETE /api/v1/session", s.security(s.handleSessionLogout))
	mux.HandleFunc("POST /api/v1/auth/enable", s.security(s.authRateLimit(s.handleAuthEnable)))
	mux.HandleFunc("POST /api/v1/auth/disable", s.security(s.auth(s.authRateLimit(s.handleAuthDisable))))
	mux.HandleFunc("POST /api/v1/auth/password", s.security(s.auth(s.authRateLimit(s.handleAuthChangePassword))))

	mux.HandleFunc("GET /healthz", s.security(s.handleHealth))

	// SPA fallback must register last so API routes win the pattern match.
	if opts.UIHandler != nil {
		mux.Handle("/", s.security(opts.UIHandler.ServeHTTP))
	}

	s.srv = &http.Server{
		Addr:              opts.Bind,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Best-effort sweep of expired sessions at startup. The store also has
	// a periodic vacuum that picks them up, so a failure here is non-fatal.
	if opts.Store != nil {
		if removed, err := opts.Store.SweepExpiredAuthSessions(context.Background()); err != nil {
			slog.Warn("auth session sweep failed", "err", err)
		} else if removed > 0 {
			slog.Info("expired auth sessions swept", "removed", removed)
		}
	}

	return s
}

// Handler exposes the wired http.Handler. Useful for httptest-driven tests.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// engine returns the current config-derived Engine snapshot. Handlers that read
// several Engine fields should call this once and reuse the result so a
// concurrent SwapEngine can't splice old and new values into one response.
func (s *Server) engine() *Engine { return s.eng.Load() }

// SwapEngine atomically replaces the config-derived Engine. Called by the
// daemon's reload controller after the new subsystems (and their pollers) are
// built and validated; the listener and in-flight live runs are untouched.
func (s *Server) SwapEngine(eng *Engine) { s.eng.Store(eng) }

// reload asks the daemon to rebuild and swap the Engine, blocking until the
// swap is live. Returns nil (no-op) when no Reload hook is wired — e.g. tests
// that don't exercise the reload path.
func (s *Server) reload(ctx context.Context) error {
	if s.opts.Reload == nil {
		slog.Warn("settings changed but no Reload hook is wired — daemon will not reload until next manual SIGHUP")
		return nil
	}
	return s.opts.Reload(ctx)
}

// Start serves until ctx is cancelled, then shuts down with a 5s grace.
func (s *Server) Start(ctx context.Context) error {
	s.baseCtx = ctx
	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "bind", s.opts.Bind)
		err := s.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
