package server

import "net/http"

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	if s.opts.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config not wired into server")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Config.Redacted())
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Version)
}
