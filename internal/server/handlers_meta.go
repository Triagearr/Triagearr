package server

import "net/http"

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	eng := s.engine()
	if eng.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config not wired into server")
		return
	}
	writeJSON(w, http.StatusOK, eng.Config.Redacted())
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Version)
}
