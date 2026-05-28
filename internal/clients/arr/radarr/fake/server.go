package fake

import (
	"io"
	"log/slog"
	"net/http"
)

// Server is a stateful fake Radarr v3 API.
type Server struct {
	state  *State
	logger *slog.Logger
}

// Options configures a Server.
type Options struct {
	APIKey string
	Logger *slog.Logger
}

// New constructs a Server with empty state.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		state:  NewState(opts.APIKey),
		logger: logger,
	}
}

// Handler returns the http.Handler exposing the fake Radarr API.
func (s *Server) Handler() http.Handler {
	return s.mux()
}

// State exposes the underlying state for seeding and assertions in tests.
func (s *Server) State() *State {
	return s.state
}
