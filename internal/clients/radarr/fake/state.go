// Package fake is an in-memory Radarr v3 API server suitable for integration
// tests and dev fixtures. Mirrors
// [github.com/Triagearr/Triagearr/internal/clients/sonarr/fake] but exposes
// movies and movie files instead of series and episode files.
package fake

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Movie mirrors the wire shape returned by /api/v3/movie. Radarr emits
// sizeOnDisk flat at the top level (no statistics nesting, unlike Sonarr).
type Movie struct {
	ID         int64       `json:"id" yaml:"id"`
	Title      string      `json:"title" yaml:"title"`
	TitleSlug  string      `json:"titleSlug" yaml:"titleSlug"`
	Path       string      `json:"path" yaml:"path"`
	SizeOnDisk int64       `json:"sizeOnDisk" yaml:"sizeOnDisk"`
	Tags       []int       `json:"tags" yaml:"tags"`
	Files      []MovieFile `json:"-" yaml:"files,omitempty"`
}

// MovieFile is one entry of /api/v3/moviefile.
type MovieFile struct {
	ID      int64  `json:"id" yaml:"id"`
	MovieID int64  `json:"movieId" yaml:"movieId"`
	Path    string `json:"path" yaml:"path"`
	Size    int64  `json:"size" yaml:"size"`
}

// Tag is one /api/v3/tag entry.
type Tag struct {
	ID    int    `json:"id" yaml:"id"`
	Label string `json:"label" yaml:"label"`
}

// HistoryRecord matches Sonarr's shape — the convert function in
// [github.com/Triagearr/Triagearr/internal/clients/arrhistory] is shared.
type HistoryRecord struct {
	ID         int64             `json:"id" yaml:"id"`
	Date       time.Time         `json:"date" yaml:"date"`
	EventType  string            `json:"eventType" yaml:"eventType"`
	DownloadID string            `json:"downloadId" yaml:"downloadId"`
	Data       HistoryRecordData `json:"data" yaml:"data"`
}

// HistoryRecordData carries the inner `data` blob Radarr returns inline.
type HistoryRecordData struct {
	FileID         string `json:"fileId,omitempty" yaml:"fileId,omitempty"`
	DownloadClient string `json:"downloadClient,omitempty" yaml:"downloadClient,omitempty"`
	DroppedPath    string `json:"droppedPath,omitempty" yaml:"droppedPath,omitempty"`
	ImportedPath   string `json:"importedPath,omitempty" yaml:"importedPath,omitempty"`
	Size           string `json:"size,omitempty" yaml:"size,omitempty"`
}

// State is the mutable in-memory store.
type State struct {
	apiKey string

	mu      sync.RWMutex
	movies  map[int64]*Movie
	tags    map[int]*Tag
	history []HistoryRecord

	movieFileDeletes atomic.Int64
}

// NewState builds an empty State.
func NewState(apiKey string) *State {
	return &State{
		apiKey: apiKey,
		movies: make(map[int64]*Movie),
		tags:   make(map[int]*Tag),
	}
}

// APIKey returns the configured API key.
func (s *State) APIKey() string { return s.apiKey }

// AddMovie upserts a movie.
func (s *State) AddMovie(m Movie) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := m
	cp.Tags = append([]int(nil), m.Tags...)
	cp.Files = append([]MovieFile(nil), m.Files...)
	s.movies[m.ID] = &cp
}

// AddTag upserts a tag.
func (s *State) AddTag(t Tag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := t
	s.tags[t.ID] = &cp
}

// AddHistory appends one history record.
func (s *State) AddHistory(h HistoryRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, h)
}

// ListMovies returns all movies (without the nested Files, which are served
// by the per-movie endpoint).
func (s *State) ListMovies() []Movie {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Movie, 0, len(s.movies))
	for _, m := range s.movies {
		cp := *m
		cp.Files = nil
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ListTags returns all tags.
func (s *State) ListTags() []Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tag, 0, len(s.tags))
	for _, t := range s.tags {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FilesForMovie returns the movie files attached to a movie, or nil if the
// movie is unknown.
func (s *State) FilesForMovie(movieID int64) []MovieFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.movies[movieID]
	if !ok {
		return nil
	}
	return append([]MovieFile(nil), m.Files...)
}

// DeleteMovieFile removes a file from its parent movie. Returns true when
// the fileID existed.
func (s *State) DeleteMovieFile(fileID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.movies {
		for i, f := range m.Files {
			if f.ID == fileID {
				m.Files = append(m.Files[:i], m.Files[i+1:]...)
				m.SizeOnDisk -= f.Size
				if m.SizeOnDisk < 0 {
					m.SizeOnDisk = 0
				}
				s.movieFileDeletes.Add(1)
				return true
			}
		}
	}
	return false
}

// MovieFileDeletes returns the number of successful DELETE /moviefile calls.
func (s *State) MovieFileDeletes() int64 {
	return s.movieFileDeletes.Load()
}

// HistoryPage returns one page of history records sorted descending by ID.
func (s *State) HistoryPage(eventType string, pageNum, pageSize int) ([]HistoryRecord, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]HistoryRecord, 0, len(s.history))
	for _, h := range s.history {
		if eventType == "" || h.EventType == eventType {
			filtered = append(filtered, h)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID > filtered[j].ID })
	total := len(filtered)
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 500
	}
	start := (pageNum - 1) * pageSize
	if start >= total {
		return nil, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total
}
