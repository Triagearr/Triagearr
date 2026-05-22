package fake

import (
	"io"
	"log/slog"
	"net/http"
)

// Server is a stateful fake qBit. Mount via Handler() into httptest or wrap
// in a real http.Server (cmd/devfixtures does the latter).
type Server struct {
	state  *State
	logger *slog.Logger
}

// Options configures a Server. Username/Password empty → auth bypass.
type Options struct {
	Username string
	Password string
	Logger   *slog.Logger
}

// New constructs a Server with an empty state. Seed via Server.State().Add.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		// Silent by default so tests don't dump output.
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		state:  NewState(opts.Username, opts.Password),
		logger: logger,
	}
}

// Handler returns the http.Handler exposing the fake qBit API.
func (s *Server) Handler() http.Handler {
	return s.mux()
}

// State exposes the underlying state for seeding and assertions in tests.
func (s *Server) State() *State {
	return s.state
}
