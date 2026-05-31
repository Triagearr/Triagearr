package fake

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Whisparr v3 uses the same numeric eventType filters as Radarr.
var eventTypeFilterToName = map[string]string{
	"1": "grabbed",
	"3": "downloadFolderImported",
	"4": "downloadFailed",
	"5": "movieFileDeleted",
}

func (s *Server) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v3/health", s.requireKey(s.handleHealth))
	m.HandleFunc("GET /api/v3/movie", s.requireKey(s.handleMovies))
	m.HandleFunc("GET /api/v3/tag", s.requireKey(s.handleTags))
	m.HandleFunc("GET /api/v3/moviefile", s.requireKey(s.handleMovieFiles))
	m.HandleFunc("DELETE /api/v3/moviefile/{id}", s.requireKey(s.handleDeleteMovieFile))
	m.HandleFunc("GET /api/v3/history", s.requireKey(s.handleHistory))
	m.HandleFunc("/", s.handleUnknown)
	return m
}

func (s *Server) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := s.state.APIKey()
		if want == "" {
			next(w, r)
			return
		}
		got := r.Header.Get("X-Api-Key")
		if got != want {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, []any{})
}

func (s *Server) handleMovies(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.state.ListMovies())
}

func (s *Server) handleTags(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.state.ListTags())
}

func (s *Server) handleMovieFiles(w http.ResponseWriter, r *http.Request) {
	movieIDRaw := r.URL.Query().Get("movieId")
	if movieIDRaw == "" {
		http.Error(w, "movieId required", http.StatusBadRequest)
		return
	}
	movieID, err := strconv.ParseInt(movieIDRaw, 10, 64)
	if err != nil {
		http.Error(w, "invalid movieId", http.StatusBadRequest)
		return
	}
	files := s.state.FilesForMovie(movieID)
	if files == nil {
		writeJSON(w, []MovieFile{})
		return
	}
	writeJSON(w, files)
}

func (s *Server) handleDeleteMovieFile(w http.ResponseWriter, r *http.Request) {
	idRaw := r.PathValue("id")
	fileID, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	addExcl := r.URL.Query().Get("addImportExclusion") == "true"
	ok := s.state.DeleteMovieFile(fileID)
	s.logger.Info("fake-whisparr_v3: delete moviefile",
		slog.Int64("id", fileID),
		slog.Bool("delete_files", deleteFiles),
		slog.Bool("add_exclusion", addExcl),
		slog.Bool("removed", ok),
	)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	eventFilter := ""
	if et := q.Get("eventType"); et != "" {
		if name, ok := eventTypeFilterToName[et]; ok {
			eventFilter = name
		} else {
			eventFilter = strings.ToLower(et)
		}
	}
	page := parseIntDefault(q.Get("page"), 1)
	pageSize := parseIntDefault(q.Get("pageSize"), 500)

	records, total := s.state.HistoryPage(eventFilter, page, pageSize)
	resp := struct {
		Page          int             `json:"page"`
		PageSize      int             `json:"pageSize"`
		SortKey       string          `json:"sortKey"`
		SortDirection string          `json:"sortDirection"`
		TotalRecords  int             `json:"totalRecords"`
		Records       []HistoryRecord `json:"records"`
	}{
		Page:          page,
		PageSize:      pageSize,
		SortKey:       q.Get("sortKey"),
		SortDirection: q.Get("sortDirection"),
		TotalRecords:  total,
		Records:       records,
	}
	writeJSON(w, resp)
}

func (s *Server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	s.logger.Warn("fake-whisparr_v3: unimplemented endpoint",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
